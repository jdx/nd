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
	semver "github.com/jdxcode/go-semver"
	homedir "github.com/mitchellh/go-homedir"
)

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
		Dependencies map[string]string `json:"dependencies"`
		Dist         *ManifestDist     `json:"dist"`
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

func MustParsePackage(root string) *PJSON {
	pkg, err := ParsePackage(root)
	must(err)
	return pkg
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
