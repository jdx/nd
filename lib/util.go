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
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/apex/log"
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
