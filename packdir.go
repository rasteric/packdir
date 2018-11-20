// Pack a directory into a zip file with relative paths and one base directory.
// This can be used for quickly snapshoting a directory and should be compatible
// with standard zip tools.
package packdir

import (
	"archive/zip"
	"compress/flate"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/docker/go-units"
	"github.com/gosuri/uiprogress"
)

func getFilesAndFolders(path string, flags int) ([]string, int64, int64) {
	var size int64
	var errors int64
	var results []string

	visit := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			if (flags & PRINT_ERRORS) != 0 {
				log.Printf("%s\n", err)
			}
			errors += 1
			return nil
		}

		if !info.IsDir() {
			size += info.Size()
		}

		results = append(results, path)
		return nil
	}
	if (flags & VERBOSE) != 0 {
		fmt.Printf("Scanning directory... ")
	}
	filepath.Walk(path, visit)
	if (flags & VERBOSE) != 0 {
		fmt.Printf("done.\n")
	}
	return results, size, errors
}

func addFile(w *zip.Writer, file string, sourceBase string, targetBase string, buffer *[]byte, flags int) error {
	if file[0:] == "/" {
		file = file[1:]
	}
	toAdd := targetBase + "/" + strings.TrimLeft(file, sourceBase)

	if (flags & VERBOSE) != 0 {
		fmt.Printf("Compressing %s\n", file)
	}

	source, err := os.Open(file)
	defer source.Close()
	if err != nil {
		return err
	}
	stat, err := source.Stat()
	if err != nil || stat.IsDir() {
		return err
	}

	target, err := w.Create(toAdd)
	if err != nil {
		return err
	}

	_, err = io.CopyBuffer(target, source, *buffer)
	if err != nil {
		return err
	}
	return nil
}

// PackResult holds results of a packing operation. ScanErrNum represents the number of errors during file scanning,
// whereas ArchiveErrNum is the number of errors during archiving.
type PackResult struct {
	FileNum       int64
	ScanErrNum    int64
	ArchiveErrNum int64
}

// Flags to control the display and logging of events at the console.
const (
	PRINT_INFO   = 1
	PRINT_ERRORS = 2
	PROGRESSBAR  = 4
	VERBOSE      = 8
)

// Compression level.
type CompressionLevel int

// The compression levels that are available.
const (
	HUFFMAN_ONLY        = -2
	DEFAULT_COMPRESSION = -1
	NO_COMPRESSION      = 0
	LEVEL1              = 1
	LEVEL2              = 2
	LEVEL3              = 3
	LEVEL4              = 4
	LEVEL5              = 5
	LEVEL6              = 6
	LEVEL7              = 7
	LEVEL8              = 8
	LEVEL9              = 9
	FAIR_COMPRESSION    = 2
	GOOD_COMPRESSION    = 5
	BEST_COMPRESSION    = 9
)

// Pack a directory, where symlinks are not followed. The source must be a directory path and
// outfile is a file path. The targetBaseDir is a directory prefix relative which the files are stored.
// If it is omitted, then it will be "snapshot", so all files will be in snapshot/file1, snapshot/file2, etc.
// The compression level needs to be one of the zip compression levels defined by constants, otherwise it is
// set to 2. The flags are used to set the log level, e.g. PRINT_ERRORS | PRINT_INFO will print errors and general
// info, but not a progress bar and won't list files.
//
// The result of this function is a PackResult structure and an error. The error should be checked to see if the
// operation succeeded at all. The result structure contains information about individual file errors.
// It is possible for the error to be nil even though individual file errors occurred.
func Pack(source string, outfile string, targetBaseDir string,
	level CompressionLevel, flags int) (*PackResult, error) {

	if level < -2 || level > 9 {
		level = 2
		if (flags & PRINT_ERRORS) != 0 {
			log.Printf("Unsupported compression level %d, using level 2 instead.\n", level)
		}
	}

	result := new(PackResult)

	source = path.Clean(source)

	if targetBaseDir == "" {
		targetBaseDir = path.Base(source)
	}
	if targetBaseDir == "." {
		targetBaseDir = "snapshot"
	}
	if targetBaseDir[len(targetBaseDir)-1:] == "/" {
		targetBaseDir = targetBaseDir[:len(targetBaseDir)-1]
	}

	// traverse the directory and get the files
	files, total, errNum1 := getFilesAndFolders(source, flags)

	result.ScanErrNum = errNum1
	result.FileNum = total

	if (flags & PRINT_INFO) != 0 {
		fmt.Printf("Archiving %d files with total size %s, %d errors during scan.\n",
			len(files), units.HumanSize(float64(total)), errNum1)
	}

	// the output file for the archive
	outFile, err := os.Create(outfile)
	if err != nil {
		if (flags & PRINT_ERRORS) != 0 {
			log.Println(err)
		}
		result.ArchiveErrNum += 1
		return result, err
	}
	defer outFile.Close()

	// create the archive
	writer := zip.NewWriter(outFile)
	buff := make([]byte, 65536)

	writer.RegisterCompressor(zip.Deflate, func(out io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(out, int(level))
	})

	var bar *uiprogress.Bar

	if (flags & PROGRESSBAR) != 0 {
		uiprogress.Start()
		bar = uiprogress.AddBar(len(files))
		bar.AppendCompleted()
	}

	var errNum2 int64

	for i, file := range files {
		err := addFile(writer, file, source, targetBaseDir, &buff, flags)
		if err != nil {
			if (flags & PRINT_ERRORS) != 0 {
				log.Printf("%s\n", err)
			}
			errNum2 += 1
		}
		if (flags & PROGRESSBAR) != 0 {
			bar.Set(i)
		}

	}
	result.ArchiveErrNum = errNum2
	err = writer.Close()
	if err != nil {
		if (flags & PRINT_ERRORS) != 0 {
			log.Println(err)
		}
		result.ArchiveErrNum += 1
	}
	if (flags & PROGRESSBAR) != 0 {
		uiprogress.Stop()
	}
	if (flags & PRINT_INFO) != 0 {
		if result.ArchiveErrNum > 0 {
			fmt.Printf("Done, %d errors during archiving.\n", result.ArchiveErrNum)
		} else {
			fmt.Printf("Done.\n")
		}
	}
	return result, nil
}
