package main

import (
	"os"
	"os/exec"
	"strings"

	"github.com/jdxcode/nd/cmd"
	nd "github.com/jdxcode/nd/lib"
)

func main() {
	args := os.Args[1:]
	if len(args) > 0 && strings.HasPrefix(args[0], ":") {
		cmd.Execute()
		return
	}
	nd.LoadProject("")
	proc := exec.Command("node", args...)
	proc.Stdin = os.Stdin
	proc.Stdout = os.Stdout
	proc.Stderr = os.Stderr
	must(proc.Run())
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
