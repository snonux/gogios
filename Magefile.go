//go:build mage
// +build mage

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/magefile/mage/mg"
)

const (
	// SSH targets for native packaging
	freebsdHost = "f0.lan.buetow.org"
	freebsdSSH  = "-p 22"
	openbsdHost = "rex@fishfinger.buetow.org"

	// NFS path on freebsdHost where the PV is mounted
	pvBase = "/data/nfs/k3svolumes/pkgrepo"

	// Repo URL paths
	freebsdRepoPath = "freebsd/FreeBSD:15:amd64/latest"
	openbsdRepoPath = "openbsd/7.8/packages/amd64"
)

// version reads the version from internal/version.go, stripping the "v" prefix.
func version() (string, error) {
	data, err := os.ReadFile("internal/version.go")
	if err != nil {
		return "", fmt.Errorf("reading version.go: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, "Version") && strings.Contains(line, "=") {
			v := strings.Trim(strings.Split(line, "\"")[1], "v")
			return v, nil
		}
	}
	return "", fmt.Errorf("version not found in internal/version.go")
}

// Build builds the gogios binary for the current platform.
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

// crossBuild compiles gogios for the given GOOS and outputs to the given path.
func crossBuild(goos, output string) error {
	fmt.Printf("Cross-compiling for %s/amd64...\n", goos)
	env := os.Environ()
	env = append(env, "GOOS="+goos, "GOARCH=amd64")
	cmd := exec.Command("go", "build", "-o", output, "cmd/gogios/main.go")
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// sshRun executes a command on a remote host via SSH.
func sshRun(host, command string) error {
	args := []string{}
	if host == freebsdHost {
		args = append(args, "-p", "22")
	}
	args = append(args, host, command)
	cmd := exec.Command("ssh", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// scpTo copies a local file to a remote host via SCP.
func scpTo(localPath, host, remotePath string) error {
	args := []string{}
	if host == freebsdHost {
		args = append(args, "-P", "22")
	}
	args = append(args, localPath, host+":"+remotePath)
	cmd := exec.Command("scp", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// scpFrom copies a remote file to a local path via SCP.
func scpFrom(host, remotePath, localPath string) error {
	args := []string{}
	if host == freebsdHost {
		args = append(args, "-P", "22")
	}
	args = append(args, host+":"+remotePath, localPath)
	cmd := exec.Command("scp", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// BuildFreebsd cross-compiles gogios for FreeBSD amd64.
func BuildFreebsd() error {
	return crossBuild("freebsd", "gogios-freebsd")
}

// BuildOpenbsd cross-compiles gogios for OpenBSD amd64.
func BuildOpenbsd() error {
	return crossBuild("openbsd", "gogios-openbsd")
}

// PkgFreebsd builds the FreeBSD package on f0 and uploads it to the repo.
// Cross-compiles locally, ships the binary to f0, packages with pkg create,
// regenerates repo metadata, and copies to the PV.
func PkgFreebsd() error {
	if err := BuildFreebsd(); err != nil {
		return err
	}

	ver, err := version()
	if err != nil {
		return err
	}

	fmt.Printf("Packaging gogios %s for FreeBSD...\n", ver)

	// Write a local temp script, scp it, then execute via /bin/sh on f0
	script := fmt.Sprintf(`#!/bin/sh
set -e
cd /tmp && rm -rf gogios-pkg
mkdir -p gogios-pkg/stage/usr/local/bin gogios-pkg/out/All
cp /tmp/gogios gogios-pkg/stage/usr/local/bin/gogios
chmod 755 gogios-pkg/stage/usr/local/bin/gogios

printf 'bin/gogios\n' > gogios-pkg/plist

cat > gogios-pkg/+MANIFEST <<MANIFEST
name: gogios
version: "%s"
origin: local/gogios
comment: "Monitoring tool with email alerts and HTML status page"
desc: "Gogios is a lightweight monitoring tool written in Go."
maintainer: "paul@buetow.org"
www: "https://codeberg.org/snonux/gogios"
prefix: /usr/local
MANIFEST

doas pkg create -M gogios-pkg/+MANIFEST -p gogios-pkg/plist -r gogios-pkg/stage -o gogios-pkg/out/All
doas pkg repo gogios-pkg/out
doas cp -Rf gogios-pkg/out/* %s/%s/
rm -rf gogios-pkg /tmp/gogios
echo "FreeBSD package gogios-%s uploaded to repo"
`, ver, pvBase, freebsdRepoPath, ver)

	if err := os.WriteFile("/tmp/pkgfreebsd.sh", []byte(script), 0o755); err != nil {
		return err
	}
	defer os.Remove("/tmp/pkgfreebsd.sh")

	if err := scpTo("gogios-freebsd", freebsdHost, "/tmp/gogios"); err != nil {
		return fmt.Errorf("scp binary to f0: %w", err)
	}
	if err := scpTo("/tmp/pkgfreebsd.sh", freebsdHost, "/tmp/pkgfreebsd.sh"); err != nil {
		return fmt.Errorf("scp script to f0: %w", err)
	}

	return sshRun(freebsdHost, "/bin/sh /tmp/pkgfreebsd.sh && rm /tmp/pkgfreebsd.sh")
}

// PkgOpenbsd builds the OpenBSD package on fishfinger and uploads it to the repo.
// Cross-compiles locally, ships the binary to fishfinger, packages with pkg_create,
// then copies the .tgz to the PV via f0.
func PkgOpenbsd() error {
	if err := BuildOpenbsd(); err != nil {
		return err
	}

	ver, err := version()
	if err != nil {
		return err
	}

	fmt.Printf("Packaging gogios %s for OpenBSD...\n", ver)

	// Ship binary to fishfinger
	if err := scpTo("gogios-openbsd", openbsdHost, "/tmp/gogios"); err != nil {
		return fmt.Errorf("scp binary to fishfinger: %w", err)
	}

	// Write a local temp script, scp it, then execute on fishfinger
	script := fmt.Sprintf(`#!/bin/sh
set -e
cd /tmp && doas rm -rf gogios-pkg
mkdir -p gogios-pkg/stage/usr/local/bin gogios-pkg/out
cp /tmp/gogios gogios-pkg/stage/usr/local/bin/gogios
chmod 755 gogios-pkg/stage/usr/local/bin/gogios

printf 'usr/local/bin/gogios\n' > gogios-pkg/plist

cat > gogios-pkg/desc <<DESC
Gogios is a lightweight monitoring tool written in Go. It runs checks
(ping, HTTP, TLS, DNS, etc.) and sends email alerts and generates an
HTML status page.
DESC

doas pkg_create \
    -D COMMENT="Monitoring tool with email alerts and HTML status page" \
    -d gogios-pkg/desc \
    -f gogios-pkg/plist \
    -B gogios-pkg/stage \
    -p / \
    gogios-pkg/out/gogios-%s.tgz
echo "OpenBSD package built"
`, ver)

	if err := os.WriteFile("/tmp/pkgopenbsd.sh", []byte(script), 0o755); err != nil {
		return err
	}
	defer os.Remove("/tmp/pkgopenbsd.sh")

	if err := scpTo("/tmp/pkgopenbsd.sh", openbsdHost, "/tmp/pkgopenbsd.sh"); err != nil {
		return fmt.Errorf("scp script to fishfinger: %w", err)
	}

	if err := sshRun(openbsdHost, "/bin/sh /tmp/pkgopenbsd.sh && rm /tmp/pkgopenbsd.sh"); err != nil {
		return fmt.Errorf("building OpenBSD package: %w", err)
	}

	// Copy package from fishfinger to local /tmp, then to f0's PV
	localTgz := fmt.Sprintf("/tmp/gogios-%s.tgz", ver)
	remoteTgz := fmt.Sprintf("/tmp/gogios-pkg/out/gogios-%s.tgz", ver)

	if err := scpFrom(openbsdHost, remoteTgz, localTgz); err != nil {
		return fmt.Errorf("scp from fishfinger: %w", err)
	}

	if err := scpTo(localTgz, freebsdHost, localTgz); err != nil {
		return fmt.Errorf("scp to f0: %w", err)
	}

	pvDest := fmt.Sprintf("%s/%s/", pvBase, openbsdRepoPath)
	if err := sshRun(freebsdHost, fmt.Sprintf("doas cp %s %s", localTgz, pvDest)); err != nil {
		return fmt.Errorf("copying to PV: %w", err)
	}

	// Clean up
	os.Remove(localTgz)
	sshRun(openbsdHost, "doas rm -rf /tmp/gogios-pkg /tmp/gogios")
	sshRun(freebsdHost, fmt.Sprintf("rm -f %s", localTgz))

	fmt.Printf("OpenBSD package gogios-%s uploaded to repo\n", ver)
	return nil
}

// Pkg builds and uploads packages for both FreeBSD and OpenBSD.
func Pkg() error {
	if err := PkgFreebsd(); err != nil {
		return err
	}
	return PkgOpenbsd()
}

// DeployOpenbsd copies the gogios binary to the frontends conf directory (legacy).
func DeployOpenbsd() error {
	fmt.Println("Copying binary...")
	cpCmd := exec.Command("cp", "gogios-openbsd", "/home/paul/git/conf/frontends/usr/local/bin/gogios")
	cpCmd.Stdout = os.Stdout
	cpCmd.Stderr = os.Stderr
	return cpCmd.Run()
}

// Openbsd builds and deploys the gogios binary for OpenBSD (legacy).
// Prefer PkgOpenbsd for package-based deployment.
func Openbsd() error {
	if err := BuildOpenbsd(); err != nil {
		return err
	}
	return DeployOpenbsd()
}
