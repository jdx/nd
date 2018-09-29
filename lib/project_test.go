package lib

import "testing"

func TestProject(t *testing.T) {
	LoadProject("../fixtures/1-example")
	LoadProject("../fixtures/2-circ")
	LoadProject("../fixtures/3-package-lock")
}
