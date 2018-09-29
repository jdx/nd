package lib

import (
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"sync"

	"github.com/apex/log"
	"github.com/disiqueira/gotree"
	"github.com/jdxcode/clonedir"
	semver "github.com/jdxcode/go-semver"
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

	dist  *ManifestDist
	pjson *PJSON
}

type Dist struct {
	Name    string
	Version string
	Sha     string
	Tarball string
}

func LoadProject(root string) *Project {
	root, err := filepath.Abs(root)
	must(err)
	log.Debugf("LoadProject", root)
	p := &Project{Root: root}
	p.resolve()
	fmt.Println(p.Debug())
	p.dedupe(nil)
	fmt.Println(p.Debug())
	p.install(p.Root)
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
	d.Lock()
	defer d.Unlock()
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
	d.Lock()
	defer d.Unlock()
	if !fileExists(d.cacheLocation()) {
		extractTarFromUrl(d.dist.Tarball, d.cacheLocation())
		setIntegrity(d.cacheLocation(), d.dist.Integrity)
	}
}

func (d *Dependency) cacheLocation() string {
	return path.Join(tmpDir, "packages", d.Name, d.Version.String())
}

func buildDepsArr(deps map[string]string) Dependencies {
	arr := Dependencies{}
	for name, v := range deps {
		arr = append(arr, &Dependency{
			Name:  name,
			Range: semver.MustParseRange(v),
			Mutex: &sync.Mutex{},
		})
	}
	return arr
}

func buildTree() {
	// input node
	// for every dep in graph:
	//   if parent has dep, continue
	//   add to tree
	//   add all descendents as well if possible
	// for every dep in tree:
	//   buildTree(dep)
}

func rsolve() {
	// use wait group
	// input: name, semver range, ancestors
	// grab manifest
	// find appropriate version
	// send cache job: cache(url) // probably buffer these
	// if deps of this not in project:
	//   wg.Add(1)
	//   go resolve(name, semverRange, ancestors+1)
	// wg.Done()
}

func install() {
	// use wait group
	// input: tree
	// for node in tree:
	//   wg.Add(1)
	//   go install(node)
	// for node in graph:
	//   wait for install to complete
	// install(node.cacheDir, node.toDir)
	// wg.Done()
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
