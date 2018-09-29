package lib

import (
	"fmt"
	"math/rand"
	"os"
	"path"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/apex/log"
	"github.com/disiqueira/gotree"
	"github.com/jdxcode/clonedir"
	semver "github.com/jdxcode/go-semver"
	"github.com/nightlyone/lockfile"
)

type Project struct {
	*Dependency

	Root        string
	YarnLock    interface{}
	PackageLock interface{}
}

type Dependency struct {
	*sync.Mutex

	Name         string
	Version      *semver.Version
	Range        *semver.Range
	Dependencies Dependencies

	dist     *ManifestDist
	pjson    *PJSON
	lockfile lockfile.Lockfile
	cacheWG  *sync.WaitGroup
}

type Dist struct {
	Name    string
	Version string
	Sha     string
	Tarball string
}

func LoadProject(root string) *Project {
	cache = sync.Map{}
	root, err := filepath.Abs(root)
	must(err)
	log.Debugf("load: %s", root)
	p := &Project{Root: root}
	p.resolve()
	fmt.Println(p.Debug())
	p.dedupe(nil)
	fmt.Println(p.Debug())
	p.install(p.Root)
	p.Wait()
	return p
}

func (p *Project) resolve() {
	log.Infof("finding all deps")
	pjson := MustParsePackage(p.Root)
	version, _ := semver.Parse(pjson.Version)
	p.Dependency = &Dependency{
		Mutex:   &sync.Mutex{},
		pjson:   pjson,
		Name:    pjson.Name,
		Version: version,
		cacheWG: &sync.WaitGroup{},
	}
	p.Dependencies = buildDepsArr(p.pjson.Dependencies)
	if p.pjson.DevDependencies != nil {
		p.Dependencies = append(p.Dependencies, buildDepsArr(*p.pjson.DevDependencies)...)
	}
	wg := sync.WaitGroup{}
	p.Sort()
	for _, dep := range p.Dependencies {
		wg.Add(1)
		go func(dep *Dependency) {
			dep.findDependents(Dependencies{})
			wg.Done()
		}(dep)
	}
	wg.Wait()
	log.Infof("found all deps")
}

func (d *Dependency) install(root string) {
	d.Lock()
	defer d.Unlock()
	for _, subdep := range d.Dependencies {
		subdep.install(path.Join(root, "node_modules", subdep.Name))
	}
	if fileExists(path.Join(root, "package.json")) {
		return
	}
	log.Infof("installing %s", d.Name)
	clonedir.Clone(d.cacheLocation(), root)
}

func (d *Dependency) findDependents(ancestors Dependencies) {
	d.Mutex.Lock()
	defer d.Mutex.Unlock()
	d.Version = getMinVersion(d.Name, d.Range)
	ancestors = append(ancestors, d)
	manifest := FetchManifest(d.Name)
	version := manifest.Versions[d.Version.String()]
	d.dist = version.Dist
	d.Dependencies = buildDepsArr(version.Dependencies)
	wg := sync.WaitGroup{}
	d.Filter(func(dep *Dependency) bool {
		return !dep.hasValidAncestor(ancestors)
	})
	d.Sort()
	for _, dep := range d.Dependencies {
		wg.Add(1)
		go func(dep *Dependency) {
			dep.findDependents(ancestors)
			wg.Done()
		}(dep)
	}
	go d.cache()
	wg.Wait()
}

func (d *Dependency) hasValidAncestor(ancestors Dependencies) bool {
	for _, ancestor := range ancestors {
		if ancestor.Name == d.Name && d.Range.Valid(ancestor.Version) {
			return true
		}
	}
	return false
}

func (d *Dependency) cache() {
	d.cacheWG.Add(1)
	defer d.cacheWG.Done()
	d.Lock()
	defer d.Unlock()
	fetch("cache_dep:"+d.cacheLocation(), func() interface{} {
		if !fileExists(path.Join(d.cacheLocation(), "package.json")) {
			extractTarFromUrl(d.dist.Tarball, d.cacheLocation())
			setIntegrity(d.cacheLocation(), d.dist.Integrity)
		}
		return nil
	})
}

func (d *Dependency) cacheLocation() string {
	return path.Join(tmpDir, "packages", d.Name, d.Version.String())
}

func buildDepsArr(deps map[string]string) Dependencies {
	arr := Dependencies{}
	for name, v := range deps {
		arr = append(arr, &Dependency{
			Name:    name,
			Range:   semver.MustParseRange(v),
			Mutex:   &sync.Mutex{},
			cacheWG: &sync.WaitGroup{},
		})
	}
	return arr
}

func (d *Dependency) Debug() string {
	var render func(d *Dependency) gotree.Tree
	render = func(d *Dependency) gotree.Tree {
		tree := gotree.New(d.String())
		for _, d := range d.Dependencies {
			tree.AddTree(render(d))
		}
		return tree
	}
	return render(d).Print()
}

func (d *Dependency) String() string {
	return fmt.Sprintf("%s@%s", d.Name, d.Version)
}

func (d *Dependency) dedupe(parent *Dependency) bool {
	for _, sub := range d.Dependencies {
		if sub.dedupe(d) {
			return d.dedupe(parent)
		}
	}
	d.Sort()
	if parent == nil {
		return false
	}
	toRemove := []string{}
	for _, sub := range d.Dependencies {
		name := sub.Name
		parentSub := parent.Get(name)
		if parentSub == nil || parentSub.Version == sub.Version {
			toRemove = append(toRemove, name)
		}
		if parentSub == nil {
			log.Debugf("adding %s", name)
			parent.Dependencies = append(parent.Dependencies, sub)
		}
	}
	for _, name := range toRemove {
		log.Debugf("removing %s", name)
		d.Remove(name)
	}
	d.Sort()
	return len(toRemove) != 0
}

func (d *Dependency) Sort() {
	sort.Sort(d.Dependencies)
}

func (d *Dependency) Get(name string) *Dependency {
	for _, dep := range d.Dependencies {
		if dep.Name == name {
			return dep
		}
	}
	return nil
}

func (d *Dependency) Remove(name string) {
	d.Filter(func(dep *Dependency) bool {
		return dep.Name != name
	})
}

func (d *Dependency) Filter(fn func(d *Dependency) bool) {
	deps := Dependencies{}
	for _, dep := range d.Dependencies {
		if fn(dep) {
			deps = append(deps, dep)
		}
	}
	d.Dependencies = deps
}

func (d *Dependency) Lock() {
	d.Mutex.Lock()
	var err error
	if d.lockfile == "" {
		f := d.cacheLocation() + ".lock"
		os.MkdirAll(path.Dir(f), 0755)
		d.lockfile, err = lockfile.New(f)
		must(err)
	}
	var lock func(timeout time.Duration)
	lock = func(timeout time.Duration) {
		err = d.lockfile.TryLock()
		if _, ok := err.(interface{ Temporary() bool }); ok {
			if timeout < 0 {
				panic("still locked")
			}
			if timeout == 30*time.Second {
				log.Warnf("lockfile locked %s", d.lockfile)
			}
			t := time.Duration(rand.Intn(1000)) * time.Millisecond
			time.Sleep(t)
			lock(timeout - t)
		} else {
			must(err)
		}
	}
	lock(30 * time.Second)
}

func (d *Dependency) Unlock() {
	if err := d.lockfile.Unlock(); err != nil {
		log.Warnf("lockfile error: %s", err.Error())
	}
	d.Mutex.Unlock()
}

func (d *Dependency) Wait() {
	for _, subdep := range d.Dependencies {
		subdep.Wait()
	}
	d.cacheWG.Wait()
}

type Dependencies []*Dependency

func (d Dependencies) Len() int {
	return len(d)
}
func (d Dependencies) Swap(i, j int) {
	d[i], d[j] = d[j], d[i]
}
func (d Dependencies) Less(i, j int) bool {
	return d[i].Name < d[j].Name
}
