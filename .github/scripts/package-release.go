// Command package-release zips a built plugin library into the layout the
// CLIProxyAPI plugin store installer accepts, and writes its sha256 checksum.
//
// The installer rejects nested directories, absolute paths, zip-slip paths and
// multiple dynamic libraries, so the archive holds exactly one entry at the zip
// root. This tool verifies that before writing the checksum: a malformed
// archive should fail the build, not the user's install.
//
// It lives under .github/scripts so the go tool skips it in ./... builds.
package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	libraryPath := flag.String("library", "", "path to the compiled plugin library")
	archivePath := flag.String("archive", "", "path to the output zip archive")
	checksumPath := flag.String("checksum", "", "path to the output checksum file")
	flag.Parse()

	if *libraryPath == "" || *archivePath == "" || *checksumPath == "" {
		fatalf("library, archive and checksum are required")
	}
	if errPackage := packageLibrary(*libraryPath, *archivePath); errPackage != nil {
		fatalf("%v", errPackage)
	}
	if errVerify := verifyArchive(*archivePath, filepath.Base(*libraryPath)); errVerify != nil {
		fatalf("%v", errVerify)
	}
	data, errRead := os.ReadFile(*archivePath)
	if errRead != nil {
		fatalf("read archive: %v", errRead)
	}
	sum := sha256.Sum256(data)
	// sha256sum format: the checksum, two spaces, then the archive basename.
	line := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), filepath.Base(*archivePath))
	if errWrite := os.WriteFile(*checksumPath, []byte(line), 0o644); errWrite != nil {
		fatalf("write checksum: %v", errWrite)
	}
	fmt.Printf("packaged %s (%d bytes)\n", *archivePath, len(data))
}

func packageLibrary(libraryPath, archivePath string) (errPackage error) {
	library, errOpen := os.Open(libraryPath)
	if errOpen != nil {
		return fmt.Errorf("open library: %w", errOpen)
	}
	defer func() {
		if errClose := library.Close(); errClose != nil && errPackage == nil {
			errPackage = fmt.Errorf("close library: %w", errClose)
		}
	}()

	info, errStat := library.Stat()
	if errStat != nil {
		return fmt.Errorf("stat library: %w", errStat)
	}
	archive, errCreate := os.Create(archivePath)
	if errCreate != nil {
		return fmt.Errorf("create archive: %w", errCreate)
	}
	defer func() {
		if errClose := archive.Close(); errClose != nil && errPackage == nil {
			errPackage = fmt.Errorf("close archive: %w", errClose)
		}
	}()

	writer := zip.NewWriter(archive)
	header, errHeader := zip.FileInfoHeader(info)
	if errHeader != nil {
		return fmt.Errorf("zip header: %w", errHeader)
	}
	header.Name = filepath.Base(libraryPath)
	header.Method = zip.Deflate
	header.SetMode(0o755)
	entry, errEntry := writer.CreateHeader(header)
	if errEntry != nil {
		return fmt.Errorf("zip entry: %w", errEntry)
	}
	if _, errCopy := io.Copy(entry, library); errCopy != nil {
		return fmt.Errorf("copy library: %w", errCopy)
	}
	if errClose := writer.Close(); errClose != nil {
		return fmt.Errorf("close zip writer: %w", errClose)
	}
	return nil
}

// verifyArchive enforces the store's zip layout rules.
func verifyArchive(archivePath, wantName string) (errVerify error) {
	reader, errOpen := zip.OpenReader(archivePath)
	if errOpen != nil {
		return fmt.Errorf("open archive: %w", errOpen)
	}
	defer func() {
		if errClose := reader.Close(); errClose != nil && errVerify == nil {
			errVerify = fmt.Errorf("close archive: %w", errClose)
		}
	}()

	if len(reader.File) != 1 {
		names := make([]string, 0, len(reader.File))
		for _, file := range reader.File {
			names = append(names, file.Name)
		}
		return fmt.Errorf("archive must hold exactly one entry, got %d: %s",
			len(reader.File), strings.Join(names, ", "))
	}
	name := reader.File[0].Name
	if name != wantName {
		return fmt.Errorf("archive entry is %q, want %q at the zip root", name, wantName)
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("archive entry %q must not be nested", name)
	}
	switch filepath.Ext(name) {
	case ".so", ".dylib", ".dll":
	default:
		return fmt.Errorf("archive entry %q is not a dynamic library", name)
	}
	return nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
