//go:build mage
// +build mage

package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/magefile/mage/mg"
)

// Build builds the gogios binary.
func Build() error {
	fmt.Println("Building...")
	cmd := exec.Command("go", "build", "-o", "gogios", "cmd/gogios/main.go")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Dev builds the gogios binary with race detection.
func Dev() error {
	mg.Deps(Vet, Lint)
	fmt.Println("Building with race detector...")
	cmd := exec.Command("go", "build", "-race", "-o", "gogios", "cmd/gogios/main.go")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Vet runs go vet on all go files.
func Vet() error {
	fmt.Println("Vetting...")
	cmd := exec.Command("go", "vet", "./...")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Lint runs golangci-lint.
func Lint() error {
	fmt.Println("Linting...")
	cmd := exec.Command("golangci-lint", "run")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// LintInstall installs golangci-lint.
func LintInstall() error {
	fmt.Println("Installing golangci-lint...")
	cmd := exec.Command("go", "install", "github.com/golangci/golangci-lint/cmd/golangci-lint@latest")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Test runs all unit tests.
func Test() error {
	fmt.Println("Cleaning test cache...")
	cleanCmd := exec.Command("go", "clean", "-testcache")
	cleanCmd.Stdout = os.Stdout
	cleanCmd.Stderr = os.Stderr
	if err := cleanCmd.Run(); err != nil {
		return err
	}

	fmt.Println("Running tests...")
	testCmd := exec.Command("go", "test", "./...")
	testCmd.Stdout = os.Stdout
	testCmd.Stderr = os.Stderr
	return testCmd.Run()
}

// Openbsd builds and deploys the gogios binary for OpenBSD.
func Openbsd() error {
	mg.Deps(BuildOpenbsd, DeployOpenbsd)
	return nil
}

// BuildOpenbsd builds the gogios binary for OpenBSD.
func BuildOpenbsd() error {
	fmt.Println("Building for OpenBSD...")
	if err := os.Setenv("GOOS", "openbsd"); err != nil {
		return err
	}
	if err := os.Setenv("GOARCH", "amd64"); err != nil {
		return err
	}
	cmd := exec.Command("go", "build", "-o", "gogios", "cmd/gogios/main.go")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// DeployOpenbsd copies the gogios binary for OpenBSD.
func DeployOpenbsd() error {
	fmt.Println("Copying binary...")
	cpCmd := exec.Command("cp", "gogios", "/home/paul/git/conf/frontends/usr/local/bin/gogios")
	cpCmd.Stdout = os.Stdout
	cpCmd.Stderr = os.Stderr
	return cpCmd.Run()
}
