package lib

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/Masterminds/semver"
	"github.com/apex/log"
)

func Load(root string) *Package {
	pkg := Package{Root: root}
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

func init() {
	log.SetLevelFromString(envOrDefault("ND_LOG", "warn"))
}

type PJSON struct {
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Dependencies map[string]string `json:"dependencies"`
}

type Manifest struct {
	Name     string `json:"name"`
	Versions map[string]struct {
		Dist struct {
			Integrity string `json:"integrity"`
			Tarball   string `json:"tarball"`
		} `json:"dist"`
	} `json:"versions"`
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
	log.Infof("cache: computing %s", key)
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
}

func (this *Package) Refresh() {
	fetch("package.Refresh:"+this.Root, func() interface{} {
		this.Dependencies = &sync.Map{}
		pjson, err := ParsePackage(this.Root)
		if os.IsNotExist(err) {
			this.install()
			pjson, err = ParsePackage(this.Root)
		}
		must(err)
		this.PJSON = pjson
		version, err := semver.NewVersion(this.PJSON.Version)
		if this.PJSON.Version != "" {
			must(err)
		}
		this.Version = version
		this.Name = this.PJSON.Name
		deps := []*Package{}
		for name, requestedVersion := range pjson.Dependencies {
			constraint, err := semver.NewConstraint(requestedVersion)
			must(err)
			dep := this.addDep(name, constraint)
			dep.mutex.Lock()
			dep.requiredBy = append(dep.requiredBy, this)
			dep.mutex.Unlock()
			deps = append(deps, dep)
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
				dep.Refresh()
			}(dep)
		}
		wg.Wait()
		return nil
	})
}

func (this *Package) addDep(name string, r *semver.Constraints) *Package {
	if this.Parent != nil {
		dep := this.Parent.addDep(name, r)
		if dep != nil {
			return dep
		}
	}
	i, loaded := this.Dependencies.LoadOrStore(name, &Package{
		Root:    path.Join(this.Root, "node_modules", name),
		Name:    name,
		Version: getMinVersion(name, r),
		Parent:  this,
		mutex:   &sync.Mutex{},
	})
	pkg := i.(*Package)
	if r.Check(pkg.Version) {
		return pkg
	}
	if !loaded {
		panic("already loaded incompatible version: " + name)
	}
	return nil
}

func FetchManifest(name string) *Manifest {
	return fetch("manifest:"+name, func() interface{} {
		url := "https://registry.npmjs.org/" + name
		rsp, err := http.Get(url)
		must(err)
		if rsp.StatusCode != 200 {
			panic("invalid status code " + url + " " + rsp.Status)
		}
		decoder := json.NewDecoder(rsp.Body)
		var manifest Manifest
		decoder.Decode(&manifest)
		return &manifest
	}).(*Manifest)
}

func (this *Package) install() {
	log.Infof("installing %s@%s to %s", this.Name, this.Version, this.Root)
	manifest := FetchManifest(this.Name)
	dist := manifest.Versions[this.Version.String()].Dist
	url := dist.Tarball
	log.Infof("%s -> %s", url, this.Root)
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
		p := strings.TrimPrefix(hdr.Name, "package/")
		p = path.Join(this.Root, p)
		must(os.MkdirAll(path.Dir(p), 0755))
		f, err := os.Create(p)
		must(err)
		_, err = io.Copy(f, tr)
		must(err)
	}
	f, err := os.Open(path.Join(this.Root, "package.json"))
	var pjson map[string]interface{}
	must(json.NewDecoder(f).Decode(&pjson))
	pjson["_integrity"] = dist.Integrity
	f, err = os.Create(path.Join(this.Root, "package.json"))
	must(err)
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	must(encoder.Encode(&pjson))
}

func getMinVersion(name string, r *semver.Constraints) *semver.Version {
	manifest := FetchManifest(name)
	parsedVersions := []*semver.Version{}
	for v := range manifest.Versions {
		parsed, err := semver.NewVersion(v)
		must(err)
		if r.Check(parsed) {
			parsedVersions = append(parsedVersions, parsed)
		}
	}
	sort.Sort(semver.Collection(parsedVersions))

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
