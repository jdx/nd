package cmd

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/Masterminds/semver"
	"github.com/spf13/cobra"
	"github.com/y0ssar1an/q"
)

func init() {
	rootCmd.AddCommand(execCmd)
}

var execCmd = &cobra.Command{
	Use:   "exec",
	Short: "run a node script",
	Long:  "equivalent to `node SCRIPT`",
	Run: func(cmd *cobra.Command, args []string) {
		refresh(project)
		proc := exec.Command("node", args...)
		proc.Stdin = os.Stdin
		proc.Stdout = os.Stdout
		proc.Stderr = os.Stderr
		must(proc.Run())
	},
}

type Package struct {
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Dependencies map[string]string `json:"dependencies"`
}

type Manifest struct {
	Versions map[string]struct {
		Dist struct {
			Tarball string `json:"tarball"`
		} `json:"dist"`
	} `json:"versions"`
}

var dependencies = sync.Map{}

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

func fetchManifest(name string) *Manifest {
	url := "https://registry.npmjs.org/" + name
	q.Q(url)
	rsp, err := http.Get(url)
	must(err)
	if rsp.StatusCode != 200 {
		panic("invalid status code " + url + " " + rsp.Status)
	}
	decoder := json.NewDecoder(rsp.Body)
	var manifest Manifest
	decoder.Decode(&manifest)
	return &manifest
}

func install(dir, name, requiredVersion string) {
	manifest := fetchManifest(name)
	version := manifest.Versions[getMaxVersion(name, requiredVersion, manifest)]
	url := version.Dist.Tarball
	q.Q(url, dir)
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

func refresh(root string) {
	wg := sync.WaitGroup{}
	ensure := func(name, requiredVersion string) {
		defer wg.Done()
		dir := path.Join(root, "node_modules", name)
		pkg := getPackage(dir)
		if pkg != nil {
			return
		}
		install(dir, name, requiredVersion)
	}
	pkg := getPackage(root)
	for name, version := range pkg.Dependencies {
		wg.Add(1)
		go ensure(name, version)
	}
	wg.Wait()
}

func getPackage(root string) *Package {
	q.Q(root)
	var pkg Package
	file, err := os.Open(path.Join(root, "package.json"))
	if err != nil && os.IsNotExist(err) {
		return nil
	}
	must(err)
	decoder := json.NewDecoder(file)
	must(decoder.Decode(&pkg))
	return &pkg
}
