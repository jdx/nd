package lib

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/apex/log"
	"github.com/jdxcode/clonedir"
	semver "github.com/jdxcode/go-semver"
	homedir "github.com/mitchellh/go-homedir"
)

func Load(root string) *Package {
	root, err := filepath.Abs(root)
	must(err)
	pjson, err := ParsePackage(root)
	must(err)
	var version *semver.Version
	if pjson.Version != "" {
		version = semver.MustParse(pjson.Version)
	}
	pkg := Package{Name: pjson.Name, Root: root, PJSON: pjson, Version: version}
	lock, err := ParsePackageLock(root)
	if lock != nil {
		pkg.PackageLock = lock
	} else {
		if !os.IsNotExist(err) {
			panic(err)
		}
	}
	pkg.Refresh()
	return &pkg
}

func envOrDefault(k, def string) string {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	return v
}

var homeDir string
var tmpDir string

func init() {
	var err error
	homeDir, err = homedir.Dir()
	must(err)
	if runtime.GOOS == "darwin" {
		tmpDir = path.Join(homeDir, "Library", "Caches", "nd")
	} else {
		tmpDir = path.Join(homeDir, ".cache", "nd")
	}
	log.SetLevelFromString(envOrDefault("ND_LOG", "warn"))
}

type PJSON struct {
	Name            string             `json:"name"`
	Version         string             `json:"version"`
	Dependencies    map[string]string  `json:"dependencies"`
	DevDependencies *map[string]string `json:"devDependencies"`
}

type PackageLock struct {
	Version      string                  `json:"version"`
	Resolved     interface{}             `json:"resolved"`
	Integrity    string                  `json:"integrity"`
	Requires     interface{}             `json:"requires"`
	Dependencies map[string]*PackageLock `json:"dependencies"`
}

type Manifest struct {
	Name     string `json:"name"`
	Versions map[string]struct {
		Dist *ManifestDist `json:"dist"`
	} `json:"versions"`
}

type ManifestDist struct {
	Integrity string `json:"integrity"`
	Tarball   string `json:"tarball"`
}

var cache = sync.Map{}

func fetch(key string, fn func() interface{}) interface{} {
	type CacheEntry struct {
		wg  *sync.WaitGroup
		val interface{}
	}
	// log.Debugf("cache: fetching %s", key)
	wg := sync.WaitGroup{}
	wg.Add(1)
	defer wg.Done()
	i, loaded := cache.LoadOrStore(key, &CacheEntry{wg: &wg})
	entry := i.(*CacheEntry)
	if loaded {
		entry.wg.Wait()
		return entry.val
	}
	// log.Infof("cache: computing %s", key)
	entry.val = fn()
	return entry.val
}

func ParsePackage(root string) (*PJSON, error) {
	// return fetch("ParsePackage:"+root, func() interface{} {
	p := path.Join(root, "package.json")
	log.Debugf("ParsePackage %s", p)
	var pkg PJSON
	file, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	if err := json.NewDecoder(file).Decode(&pkg); err != nil {
		return nil, err
	}
	return &pkg, nil
	// }).(*Package)
}

func ParsePackageLock(root string) (*PackageLock, error) {
	p := path.Join(root, "package-lock.json")
	log.Debugf("ParsePackageLock %s", p)
	var pkg PackageLock
	file, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	if err := json.NewDecoder(file).Decode(&pkg); err != nil {
		return nil, err
	}
	return &pkg, nil
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

type Package struct {
	Root         string
	Name         string
	Version      *semver.Version
	Dependencies *sync.Map
	Parent       *Package
	PJSON        *PJSON
	mutex        *sync.Mutex
	requiredBy   []*Package
	PackageLock  *PackageLock
}

func (this *Package) Refresh() {
	cache = sync.Map{}
	this.init()
	this.refresh(true)
	this.validate()
}

func (this *Package) validate() {
	for key, requestedVersion := range this.PJSON.Dependencies {
		r := semver.MustParseRange(requestedVersion)
		dep := this.require(key)
		r.Valid(dep.Version)
	}
	this.Dependencies.Range(func(key interface{}, d interface{}) bool {
		dep := d.(*Package)
		dep.validate()
		return true
	})
}

func (this *Package) init() {
	log.Debugf("%s", this.Root)
	readdir := func(dir string) []os.FileInfo {
		files, err := ioutil.ReadDir(path.Join(this.Root, "node_modules"))
		if os.IsNotExist(err) {
			return []os.FileInfo{}
		}
		must(err)
		return files
	}
	deps := []string{}
	modules := path.Join(this.Root, "node_modules")
	for _, file := range readdir(modules) {
		if !file.IsDir() {
			continue
		}
		dir := file.Name()
		if strings.HasPrefix(dir, "@") {
			for _, file2 := range readdir(path.Join(modules, dir)) {
				if !file2.IsDir() {
					continue
				}
				deps = append(deps, path.Join(dir, file2.Name()))
			}
		} else {
			deps = append(deps, dir)
		}
	}
	sort.Strings(deps)
	for _, d := range deps {
		if !fileExists(path.Join(modules, d, "package.json")) {
			continue
		}
	}
}

func (this *Package) refresh(dev bool) {
	fetch("package.Refresh:"+this.Root, func() interface{} {
		if this.Parent != nil && this.Parent.PackageLock != nil {
			deps := this.Parent.PackageLock.Dependencies
			this.PackageLock = deps[this.Name]
		}
		this.Dependencies = &sync.Map{}
		if this.PJSON == nil {
			pjson, err := ParsePackage(this.Root)
			if os.IsNotExist(err) || !semver.MustParse(pjson.Version).EQ(this.Version) {
				this.install()
				pjson, err = ParsePackage(this.Root)
			}
			must(err)
			this.PJSON = pjson
		}
		deps := []*Package{}
		addDep := func(name, requestedVersion string) {
			dep := this.addDep(name, semver.MustParseRange(requestedVersion))
			dep.mutex.Lock()
			dep.requiredBy = append(dep.requiredBy, this)
			dep.mutex.Unlock()
			deps = append(deps, dep)
		}
		for name, requestedVersion := range this.PJSON.Dependencies {
			addDep(name, requestedVersion)
		}
		if dev && this.PJSON.DevDependencies != nil {
			for name, requestedVersion := range *this.PJSON.DevDependencies {
				addDep(name, requestedVersion)
			}
		}
		wg := sync.WaitGroup{}
		for _, dep := range deps {
			if this.isRequiredBy(dep) {
				// circular reference
				continue
			}
			wg.Add(1)
			go func(dep *Package) {
				defer wg.Done()
				dep.refresh(false)
			}(dep)
		}
		wg.Wait()
		return nil
	})
}

func (this *Package) addDep(name string, r *semver.Range) *Package {
	if this.Parent != nil {
		dep := this.Parent.addDep(name, r)
		if dep != nil {
			return dep
		}
	}
	root := path.Join(this.Root, "node_modules", name)
	i, loaded := this.Dependencies.LoadOrStore(name, &Package{
		Root:    root,
		Name:    name,
		Version: this.getIdealVersion(name, r),
		Parent:  this,
		mutex:   &sync.Mutex{},
	})
	pkg := i.(*Package)
	if r.Valid(pkg.Version) {
		return pkg
	}
	if !loaded {
		panic(fmt.Sprintf("already loaded incompatible version: %s@%s expected: %#v", name, pkg.Version.String(), r))
	}
	return nil
}

type FileList []os.FileInfo

func (fl FileList) Len() int {
	return len(fl)
}

func (fl FileList) Swap(i, j int) {
	fl[i], fl[j] = fl[j], fl[i]
}
func (fl FileList) Less(i, j int) bool {
	return fl[i].ModTime().Before(fl[j].ModTime())
}

func FetchManifest(name string) *Manifest {
	return fetch("manifest:"+name, func() interface{} {
		var manifest Manifest
		cacheRoot := path.Join(tmpDir, "manifests", name)
		etag := func() string {
			var files FileList
			var err error
			files, err = ioutil.ReadDir(cacheRoot)
			if os.IsNotExist(err) {
				return ""
			}
			must(err)
			sort.Sort(files)
			if len(files) == 0 {
				return ""
			}
			return strings.Split(files[len(files)-1].Name(), ".")[0]
		}()
		url := "https://registry.npmjs.org/" + name
		get := func(etag string) *http.Response {
			log.Debugf("HTTP GET %s", url)
			client := http.Client{}
			req, err := http.NewRequest("GET", url, nil)
			if etag != "" {
				req.Header.Set("If-None-Match", `"`+etag+`"`)
			}
			must(err)
			rsp, err := client.Do(req)
			must(err)
			log.Infof("HTTP GET %s %d", url, rsp.StatusCode)
			return rsp
		}
		rsp := get(etag)
		if rsp.StatusCode == 304 {
			cachePath := path.Join(cacheRoot, etag+".json")
			cache, err := os.Open(cachePath)
			if err == nil {
				if err = json.NewDecoder(cache).Decode(&manifest); err == nil {
					return &manifest
				}
			}
			if err != nil {
				log.Warnf("HTTP GET %s %s", url, err.Error())
				must(os.RemoveAll(cacheRoot))
				rsp = get("")
			}
		}
		if rsp.StatusCode != 200 {
			panic("invalid status code " + url + " " + rsp.Status)
		}
		etag = strings.Trim(strings.TrimLeft(rsp.Header.Get("etag"), "W/"), `"`)
		cachePath := path.Join(cacheRoot, etag+".json")
		must(os.MkdirAll(path.Dir(cachePath), 0755))
		cache, err := os.Create(cachePath)
		must(err)
		pipeIn, pipeOut := io.Pipe()
		multi := io.MultiWriter(cache, pipeOut)
		go func() {
			_, err := io.Copy(multi, rsp.Body)
			must(err)
		}()
		must(json.NewDecoder(pipeIn).Decode(&manifest))
		return &manifest
	}).(*Manifest)
}

func fileExists(p string) bool {
	if _, err := os.Stat(p); err != nil {
		if os.IsNotExist(err) {
			return false
		}
		panic(err)
	}
	return true
}

func (this *Package) install() {
	log.Debugf("installing %s@%s to %s", this.Name, this.Version, this.Root)
	version := this.Version.String()
	cacheLocation := path.Join(tmpDir, "packages", this.Name, version)

	if !fileExists(cacheLocation) {
		dist := this.getTarball()
		extractTarFromUrl(dist.Tarball, cacheLocation)
		setIntegrity(cacheLocation, dist.Integrity)
	}

	clonedir.Clone(cacheLocation, this.Root)
}

func (this *Package) getTarball() *ManifestDist {
	if dist := this.findLockTarball(this.Name, this.Version); dist != nil {
		return dist
	}
	version := this.Version.String()
	manifest := FetchManifest(this.Name)
	dist := manifest.Versions[version].Dist
	return dist
}

func (this *Package) findLockTarball(name string, version *semver.Version) *ManifestDist {
	if this.Parent != nil {
		return this.Parent.findLockTarball(name, version)
	}
	var find func(lock *PackageLock) *PackageLock
	find = func(lock *PackageLock) *PackageLock {
		if this.PackageLock == nil {
			return nil
		}
		for depName, dep := range lock.Dependencies {
			if depName == name && semver.MustParse(dep.Version).EQ(version) {
				return dep
			}
			if dep := find(dep); dep != nil {
				return dep
			}
		}
		return nil
	}
	dep := find(this.PackageLock)
	if dep == nil {
		return nil
	}
	if resolved, ok := dep.Resolved.(string); ok {
		return &ManifestDist{
			Integrity: dep.Integrity,
			Tarball:   resolved,
		}
	}
	return nil
}

func extractTarFromUrl(url, to string) {
	log.Infof("HTTP GET %s", url)
	rsp, err := http.Get(url)
	must(err)
	if rsp.StatusCode != 200 {
		panic("invalid status code " + url + " " + rsp.Status)
	}
	deflate, err := gzip.NewReader(rsp.Body)
	must(err)
	tr := tar.NewReader(deflate)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		must(err)

		// take out first part of path (/package)
		tokens := strings.SplitAfterN(hdr.Name, fmt.Sprintf("%c", filepath.Separator), 2)
		if len(tokens) < 2 {
			continue
		}
		p := tokens[1]

		fi := hdr.FileInfo()
		mode := fi.Mode()
		p = path.Join(to, p)
		must(os.MkdirAll(path.Dir(p), 0755))
		if mode.IsDir() {
			log.Debugf("creating directory %s", p)
			must(os.MkdirAll(path.Dir(p), 0755))
		} else if mode.IsRegular() {
			f, err := os.Create(p)
			must(err)
			_, err = io.Copy(f, tr)
			must(err)
		}
	}
}

func setIntegrity(root, integrity string) {
	pjsonPath := path.Join(root, "package.json")
	log.Debugf("setIntegrity(%s)", pjsonPath)
	f, err := os.Open(pjsonPath)
	var pjson map[string]interface{}
	must(json.NewDecoder(f).Decode(&pjson))
	pjson["_integrity"] = integrity
	f, err = os.Create(pjsonPath)
	must(err)
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	must(encoder.Encode(&pjson))
}

func getMinVersion(name string, r *semver.Range) *semver.Version {
	manifest := FetchManifest(name)
	parsedVersions := semver.Versions{}
	for raw := range manifest.Versions {
		v := semver.MustParse(raw)
		if r.Valid(v) {
			parsedVersions = append(parsedVersions, v)
		}
	}
	sort.Sort(parsedVersions)

	if len(parsedVersions) < 1 {
		panic("no version found for " + name)
	}

	return parsedVersions[0]
}

func (this *Package) isRequiredBy(other *Package) bool {
	if this == other {
		return true
	}
	for _, p := range this.requiredBy {
		if p.isRequiredBy(other) {
			return true
		}
	}
	return false
}

func (this *Package) getIdealVersion(name string, r *semver.Range) *semver.Version {
	if this.PackageLock != nil {
		if lock := this.PackageLock.Dependencies[name]; lock != nil {
			version := semver.MustParse(lock.Version)
			if r.Valid(version) {
				return version
			}
		}
	}
	return getMinVersion(name, r)
}

func (this *Package) getCurrentVersion(name string, r *semver.Range) *semver.Version {
	pjson, err := ParsePackage(path.Join(this.Root, "node_modules", name))
	if os.IsNotExist(err) {
		if this.Parent != nil {
			return this.Parent.getCurrentVersion(name, r)
		}
		return nil
	}
	must(err)
	version := semver.MustParse(pjson.Version)
	if r.Valid(version) {
		return version
	}
	return nil
}

func (this *Package) require(name string) *Package {
	if dep, ok := this.Dependencies.Load(name); ok {
		return dep.(*Package)
	}
	if this.Parent != nil {
		return this.Parent.require(name)
	}
	return nil
}
