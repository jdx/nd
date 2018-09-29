package lib

import (
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
)

var fixtures = path.Join("..", "fixtures")

func checkVersion(t *testing.T, p, version string) {
	pkg, err := ParsePackage(p)
	must(err)
	assert.Equal(t, version, pkg.Version)
}

func Test1Example(t *testing.T) {
	os.RemoveAll(tmpDir)
	root := path.Join(fixtures, "1-example")
	os.RemoveAll(path.Join(root, "node_modules"))
	test := func() {
		pkg := LoadProject(root)
		checkVersion(t, path.Join(root, "node_modules/nd-a"), "1.0.0")
		checkVersion(t, path.Join(root, "node_modules/edon-test-a"), "1.0.1")
		checkVersion(t, path.Join(root, "node_modules/edon-test-b"), "1.2.1")
		checkVersion(t, path.Join(root, "node_modules/edon-test-c"), "1.0.3")
		checkVersion(t, path.Join(root, "node_modules/edon-test-a/node_modules/edon-test-c"), "2.0.0")
		checkVersion(t, path.Join(root, "node_modules/edon-test-b/node_modules/edon-test-c"), "1.0.0")
		assert.Equal(t,
			`example@0.0.0
├── edon-test-a@1.0.1
│   └── edon-test-c@2.0.0
├── edon-test-b@1.2.1
│   └── edon-test-c@1.0.0
├── edon-test-c@1.0.3
└── nd-a@1.0.0
`, pkg.Debug())
	}
	test()
	os.RemoveAll(path.Join(root, "node_modules"))
	test()
	test()
	os.RemoveAll(path.Join(root, "node_modules"))
}

func Test2Circ(t *testing.T) {
	root := path.Join(fixtures, "2-circ")
	os.RemoveAll(tmpDir)
	os.RemoveAll(path.Join(root, "node_modules"))
	test := func() {
		pkg := LoadProject(root)
		checkVersion(t, path.Join(root, "node_modules/nd-circ-a"), "1.0.0")
		checkVersion(t, path.Join(root, "node_modules/nd-circ-b"), "1.0.0")
		assert.Equal(t,
			`@*
├── nd-circ-a@1.0.0
└── nd-circ-b@1.0.0
`, pkg.Debug())
	}
	test()
	os.RemoveAll(path.Join(root, "node_modules"))
	test()
	test()
	os.RemoveAll(path.Join(root, "node_modules"))
}

func Test3Lock(t *testing.T) {
	root := path.Join(fixtures, "3-package-lock")
	os.RemoveAll(tmpDir)
	os.RemoveAll(path.Join(root, "node_modules"))
	test := func() {
		pkg := LoadProject(root)
		checkVersion(t, path.Join(root, "node_modules/nd-a"), "1.0.0")
		checkVersion(t, path.Join(root, "node_modules/edon-test-a"), "0.0.1")
		checkVersion(t, path.Join(root, "node_modules/edon-test-b"), "0.0.1")
		checkVersion(t, path.Join(root, "node_modules/edon-test-c"), "1.0.3")
		checkVersion(t, path.Join(root, "node_modules/edon-test-a/node_modules/edon-test-c"), "0.0.0")
		checkVersion(t, path.Join(root, "node_modules/edon-test-b/node_modules/edon-test-c"), "0.0.0")
		assert.Equal(t,
			`example@0.0.0
├── edon-test-a@0.0.1
│   └── edon-test-c@0.0.0
├── edon-test-b@0.0.1
│   └── edon-test-c@0.0.0
├── edon-test-c@1.0.3
└── nd-a@1.0.0
`, pkg.Debug())
	}
	test()
	os.RemoveAll(path.Join(root, "node_modules"))
	test()
	test()
	os.RemoveAll(path.Join(root, "node_modules"))
}
