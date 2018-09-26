package lib

import (
	"encoding/json"
	"net/http"
	"os"
	"path"
	"sort"
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
	log.Debugf("cache: computing %s", key)
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

func ParsePackage(root string) *Package {
	return fetch("ParsePackage:"+root, func() interface{} {
		p := path.Join(root, "package.json")
		log.Debugf("ParsePackage %s", p)
		var pkg Package
		file, err := os.Open(p)
		if err != nil && os.IsNotExist(err) {
			return nil
		}
		must(err)
		decoder := json.NewDecoder(file)
		must(decoder.Decode(&pkg))
		return &pkg
	}).(*Package)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
