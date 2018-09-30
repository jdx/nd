package lib

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/apex/log"
	semver "github.com/jdxcode/go-semver"
)

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

func getMaxVersion(name string, r *semver.Range) *semver.Version {
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
		panic(fmt.Errorf("no version found for %s@%s", name, r))
	}

	return parsedVersions[len(parsedVersions)-1]
}

func FetchManifest(name string) *Manifest {
	return fetch("manifest:"+name, func() interface{} {
		startNetworking()
		defer stopNetworking()
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
