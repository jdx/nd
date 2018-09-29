package lib

import (
	"os"
	"testing"
)

func TestProject(t *testing.T) {
	LoadProject("../fixtures/1-example")
	os.RemoveAll("../fixtures/1-example/node_modules")
	LoadProject("../fixtures/2-circ")
	os.RemoveAll("../fixtures/2-circ/node_modules")
	LoadProject("../fixtures/3-package-lock")
	os.RemoveAll("../fixtures/3-package-lock/node_modules")
}
