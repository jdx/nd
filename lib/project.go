package lib

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/apex/log"
	semver "github.com/jdxcode/go-semver"
)

type Project struct {
	*Dependency

	Root        string
	YarnLock    interface{}
	PackageLock interface{}
}

func LoadProject(root string) *Project {
	setPool(20)
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
