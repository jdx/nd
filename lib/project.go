package lib

import (
	"fmt"
	"path"
	"path/filepath"
	"sync"

	"github.com/apex/log"
	"github.com/disiqueira/gotree"
	semver "github.com/jdxcode/go-semver"
)

type Project struct {
	*Dependency
	Root        string
	YarnLock    interface{}
	PackageLock interface{}
}

type Dependency struct {
	Name         string
	Version      *semver.Version
	Range        *semver.Range
	Dependencies []*Dependency

	ancestors []*Dependency
	lock      *sync.Mutex
	dist      *ManifestDist
	pjson     *PJSON
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
	// resolving is done so build the tree
	// tree = buildIdealTree()
	// once caching is complete:
	//   install(tree)
	// go init()
	// for {
	// 	select {
	// 	case job, open := <-p.resolveJobs:
	// 	case job, open := <-p.cacheJobs:
	// 	}
	// }
	return p
}

func (p *Project) resolve() {
	log.Infof("finding all deps")
	pjson := MustParsePackage(p.Root)
	version, _ := semver.Parse(pjson.Version)
	p.Dependency = &Dependency{
		pjson:   pjson,
		Name:    pjson.Name,
		Version: version,
	}
	p.Dependencies = buildDepsArr([]*Dependency{}, p.pjson.Dependencies)
	if p.pjson.DevDependencies != nil {
		p.Dependencies = append(p.Dependencies, buildDepsArr([]*Dependency{}, *p.pjson.DevDependencies)...)
	}
	wg := sync.WaitGroup{}
	for _, dep := range p.Dependencies {
		wg.Add(1)
		go func(dep *Dependency) {
			dep.findDependents()
			wg.Done()
		}(dep)
	}
	wg.Wait()
	log.Infof("found all deps")
}

func (d *Dependency) findDependents() {
	d.lock.Lock()
	defer d.lock.Unlock()
	d.Version = getMinVersion(d.Name, d.Range)
	for _, ancestor := range d.ancestors {
		if ancestor.Name == d.Name && ancestor.Version == d.Version {
			// circular
			return
		}
	}
	ancestors := append(d.ancestors, d)
	manifest := FetchManifest(d.Name)
	version := manifest.Versions[d.Version.String()]
	d.dist = version.Dist
	d.Dependencies = buildDepsArr(ancestors, version.Dependencies)
	wg := sync.WaitGroup{}
	for _, dep := range d.Dependencies {
		wg.Add(1)
		go func(dep *Dependency) {
			dep.findDependents()
			wg.Done()
		}(dep)
	}
	wg.Wait()
}

func (d *Dependency) cache() {
	d.lock.Lock()
	defer d.lock.Unlock()
	cacheLocation := path.Join(tmpDir, "packages", d.Name, d.Version.String())
	if !fileExists(cacheLocation) {
		extractTarFromUrl(d.dist.Tarball, cacheLocation)
		setIntegrity(cacheLocation, d.dist.Integrity)
	}
}

func buildDepsArr(ancestors []*Dependency, deps map[string]string) []*Dependency {
	arr := []*Dependency{}
	for name, v := range deps {
		arr = append(arr, &Dependency{
			Name:      name,
			Range:     semver.MustParseRange(v),
			ancestors: ancestors,
			lock:      &sync.Mutex{},
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
