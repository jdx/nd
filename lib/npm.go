package lib

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
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

type Package struct {
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Dependencies map[string]string `json:"dependencies"`
}

type Manifest struct {
	Name     string `json:"name"`
	Versions map[string]struct {
		Dist struct {
			Tarball string `json:"tarball"`
		} `json:"dist"`
	} `json:"versions"`
}

var cache = sync.Map{}

func getMaxVersion(name, r string, manifest *Manifest) string {
	rng, err := semver.NewConstraint(r)
	must(err)
	parsedVersions := []*semver.Version{}
	for v := range manifest.Versions {
		parsed, err := semver.NewVersion(v)
		must(err)
		if rng.Check(parsed) {
			parsedVersions = append(parsedVersions, parsed)
		}
	}
	sort.Sort(semver.Collection(parsedVersions))

	if len(parsedVersions) < 1 {
		panic("no version found for " + name)
	}

	return parsedVersions[len(parsedVersions)-1].String()
}

func fetch(key string, fn func() interface{}) interface{} {
	log.Debugf("cache: fetching %s", key)
	wgOrManifest, loaded := cache.LoadOrStore(key, &sync.WaitGroup{})
	if loaded {
		if manifest, ok := wgOrManifest.(*Manifest); ok {
			log.Debugf("cache: found %s", key)
			return manifest
		}
		log.Debugf("cache: waiting for %s", key)
		wg := wgOrManifest.(*sync.WaitGroup)
		wg.Wait()
		return fetch(key, fn)
	}
	wg := wgOrManifest.(*sync.WaitGroup)
	wg.Add(1)
	defer wg.Done()
	log.Infof("cache: computing %s", key)
	result := fn()
	// log.Debugf("cache: computed %s", key)
	cache.Store(key, result)
	return result
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

func ParsePackage(root string) (*Package, error) {
	// return fetch("ParsePackage:"+root, func() interface{} {
	p := path.Join(root, "package.json")
	log.Debugf("ParsePackage %s", p)
	var pkg Package
	file, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(file)
	err = decoder.Decode(&pkg)
	if err != nil {
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

func install(dir, name, requiredVersion string) {
	manifest := FetchManifest(name)
	version := manifest.Versions[getMaxVersion(name, requiredVersion, manifest)]
	url := version.Dist.Tarball
	log.Infof("%s -> %s", url, dir)
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
		p = path.Join(dir, p)
		must(os.MkdirAll(path.Dir(p), 0755))
		f, err := os.Create(p)
		must(err)
		_, err = io.Copy(f, tr)
		must(err)
	}
}

func Refresh(root string) {
	var refresh func(root string, wg *sync.WaitGroup)
	refresh = func(root string, wg *sync.WaitGroup) {
		log.Debugf("refresh %s", root)
		ensure := func(name, requiredVersion string) {
			fmt.Println(root)
			defer wg.Done()
			dir := path.Join(root, "node_modules", name)
			_, err := ParsePackage(dir)
			if os.IsNotExist(err) {
				install(dir, name, requiredVersion)
			}
			refresh(dir, wg)
		}
		pkg, err := ParsePackage(root)
		must(err)
		for name, version := range pkg.Dependencies {
			wg.Add(1)
			go ensure(name, version)
		}
	}
	wg := sync.WaitGroup{}
	refresh(root, &wg)
	wg.Wait()
}
