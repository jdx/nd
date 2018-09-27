package main

import (
	"os"
	"os/exec"

	nd "github.com/jdxcode/nd/lib"
)

func main() {
	nd.Load("")
	args := os.Args[1:]
	proc := exec.Command("node", args...)
	proc.Stdin = os.Stdin
	proc.Stdout = os.Stdout
	proc.Stderr = os.Stderr
	must(proc.Run())
	// cmd.Execute()
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
