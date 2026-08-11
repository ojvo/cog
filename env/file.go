package env

import (
	"bufio"
	"container/list"
	"crypto/sha1"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Dir represents a directory structure with subdirectories and files
type Dir struct {
	Name    string   `json:"name"`
	Folders []*Dir   `json:"folders,omitempty"`
	Files   []string `json:"files,omitempty"`
}

// ========== 文件/目录检查 ==========

// Exists checks whether a file or directory exists
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// IsFile returns true if path exists and is a file
func IsFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// IsDir returns true if path exists and is a directory
func IsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// ========== 文件信息获取 ==========

// FileInfo contains file information
type FileInfo struct {
	Size    int64       `json:"size"`
	ModTime int64       `json:"mod_time"`
	Mode    os.FileMode `json:"mode"`
	IsDir   bool        `json:"is_dir"`
}

// GetFileInfo returns comprehensive file information
func GetFileInfo(path string) (*FileInfo, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to get file info for %s: %w", path, err)
	}

	return &FileInfo{
		Size:    stat.Size(),
		ModTime: stat.ModTime().Unix(),
		Mode:    stat.Mode(),
		IsDir:   stat.IsDir(),
	}, nil
}

// GetFileSize returns the size of the file in bytes
func GetFileSize(path string) (int64, error) {
	info, err := GetFileInfo(path)
	if err != nil {
		return -1, err
	}
	return info.Size, nil
}

// GetFileModTime returns file modified time as Unix timestamp
func GetFileModTime(path string) (int64, error) {
	info, err := GetFileInfo(path)
	if err != nil {
		return 0, err
	}
	return info.ModTime, nil
}

// GetFileMode returns file mode
func GetFileMode(path string) (os.FileMode, error) {
	info, err := GetFileInfo(path)
	if err != nil {
		return 0, err
	}
	return info.Mode, nil
}

// ========== 文件内容处理 ==========

// CountLines counts the number of lines in a file efficiently
func CountLines(path string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("could not open file %s: %w", path, err)
	}
	defer file.Close()

	// 用 bufio.Reader 替代 Scanner：Scanner 默认 64KB 行限制，
	// 超长行（如 minified JS、单行 JSON）会报 bufio.ErrTooLong。
	// ReadString 逐块读取直到遇到分隔符，无行长度上限。
	reader := bufio.NewReader(file)
	count := 0
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			count++
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return count, fmt.Errorf("error reading file %s: %w", path, err)
		}
	}
	return count, nil
}

// ReadLines reads a file and returns all lines as a slice of strings.
// Lines are trimmed of trailing \r\n. Blank lines are preserved. The last
// line is included even if it does not end with a newline.
// Uses bufio.Reader to avoid the 64KB line-length limit of bufio.Scanner.
func ReadLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", path, err)
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	var lines []string
	for {
		line, err := reader.ReadString('\n')
		hasNewline := err == nil
		line = strings.TrimRight(line, "\r\n")
		// Add the line if it has content or if we read a complete line
		// (preserves blank lines — a bare "\n" yields hasNewline=true).
		if len(line) > 0 || hasNewline {
			lines = append(lines, line)
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return lines, fmt.Errorf("error reading file %s: %w", path, err)
		}
	}
	return lines, nil
}

// ========== 目录操作 ==========

// CreateDir creates a directory and all necessary parent directories
func CreateDir(path string) error {
	return os.MkdirAll(path, 0755)
}

// RemoveAll removes a directory and all its contents, or removes a file
func RemoveAll(path string) error {
	return os.RemoveAll(path)
}

// ========== 文件搜索和列表 ==========

// ListOptions defines options for listing files and directories
type ListOptions struct {
	Recursive    bool     // Whether to search recursively
	Extensions   []string // File extensions to filter (e.g., ".txt", ".go")
	IncludeDirs  bool     // Whether to include directories in results
	IncludeFiles bool     // Whether to include files in results
}

// ListFiles lists files and/or directories in a path with various options
func ListFiles(dirPath string, opts *ListOptions) ([]string, error) {
	if opts == nil {
		opts = &ListOptions{
			IncludeFiles: true, // Default to including files
		}
	}

	if !IsDir(dirPath) {
		return nil, fmt.Errorf("path is not a directory: %s", dirPath)
	}

	var results []string

	// 预先 Clean 规范化根路径，避免与 Walk 产生的 path 做字符串比较时
	// 因尾随分隔符、"./" 前缀等格式差异导致误判。
	cleanRoot := filepath.Clean(dirPath)

	walkFunc := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip root directory
		if filepath.Clean(path) == cleanRoot {
			return nil
		}

		// Handle non-recursive mode
		if !opts.Recursive && filepath.Clean(filepath.Dir(path)) != cleanRoot {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Filter by type
		if info.IsDir() {
			if !opts.IncludeDirs {
				return nil
			}
		} else {
			if !opts.IncludeFiles {
				return nil
			}
			// Filter by extension for files
			if len(opts.Extensions) > 0 {
				ext := strings.ToLower(filepath.Ext(info.Name()))
				match := false
				for _, allowedExt := range opts.Extensions {
					if strings.ToLower(allowedExt) == ext {
						match = true
						break
					}
				}
				if !match {
					return nil
				}
			}
		}

		results = append(results, path)
		return nil
	}

	err := filepath.Walk(dirPath, walkFunc)
	return results, err
}

// GetFiles returns files in a directory with optional filtering and depth control
// Fixed: Now properly returns all files in current directory and respects recursion depth
func GetFiles(pathname string, filter string, maxDepth int) ([]string, error) {
	if !IsDir(pathname) {
		return nil, fmt.Errorf("path is not a directory: %s", pathname)
	}

	var fileList []string
	return getFilesRecursive(pathname, filter, maxDepth, 0, fileList)
}

// Helper function for recursive file listing with depth control
func getFilesRecursive(pathname string, filter string, maxDepth, currentDepth int, fileList []string) ([]string, error) {
	entries, err := os.ReadDir(pathname)
	if err != nil {
		return fileList, fmt.Errorf("failed to read directory %s: %w", pathname, err)
	}

	for _, entry := range entries {
		fullPath := filepath.Join(pathname, entry.Name())

		if entry.IsDir() {
			// Recurse into subdirectory if we haven't reached max depth
			if maxDepth <= 0 || currentDepth < maxDepth-1 {
				var err error
				fileList, err = getFilesRecursive(fullPath, filter, maxDepth, currentDepth+1, fileList)
				if err != nil {
					return fileList, err
				}
			}
		} else {
			// Add file if it matches filter
			if filter == "" || filepath.Ext(entry.Name()) == filter {
				fileList = append(fileList, fullPath)
			}
		}
	}

	return fileList, nil
}

// SearchFile searches for a file in multiple paths
func SearchFile(fileName string, searchPaths ...string) (string, error) {
	for _, searchPath := range searchPaths {
		fullPath := filepath.Join(searchPath, fileName)
		if Exists(fullPath) {
			return fullPath, nil
		}
	}
	return "", fmt.Errorf("file %s not found in any search path", fileName)
}

// ListDirsOnly returns only directories in the given path
func ListDirsOnly(dirPath string, depth int) ([]string, error) {
	if !IsDir(dirPath) {
		return nil, fmt.Errorf("path is not a directory: %s", dirPath)
	}

	var dirs []string
	return listDirsRecursive(dirPath, depth, 0, dirs)
}

// Helper function for recursive directory listing with depth control
func listDirsRecursive(dirPath string, maxDepth, currentDepth int, dirs []string) ([]string, error) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return dirs, fmt.Errorf("failed to read directory %s: %w", dirPath, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			fullPath := filepath.Join(dirPath, entry.Name())

			// 统一返回全路径（原实现 currentDepth==0 返回相对名，与深层
			// 返回全路径不一致，调用者无法统一处理）
			dirs = append(dirs, fullPath+"/")

			// Recurse if we haven't reached max depth
			if maxDepth <= 0 || currentDepth < maxDepth-1 {
				var err error
				// 传入 dirs 而非空切片，保持有序且不丢失已收集结果；
				// 传播 error 而非吞掉（原实现用 _ 忽略，导致子目录读取
				// 失败时调用者无感知）
				dirs, err = listDirsRecursive(fullPath, maxDepth, currentDepth+1, dirs)
				if err != nil {
					return dirs, err
				}
			}
		}
	}

	return dirs, nil
}

// ========== 文件操作 ==========

// CopyFile copies a file from source to destination, preserving permissions
func CopyFile(src, dst string) error {
	srcInfo, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("failed to get source file info: %w", err)
	}

	// Handle symbolic links
	if srcInfo.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return fmt.Errorf("failed to read symlink: %w", err)
		}
		// Ensure destination directory exists
		if err := CreateDir(filepath.Dir(dst)); err != nil {
			return fmt.Errorf("failed to create destination directory: %w", err)
		}
		return os.Symlink(target, dst)
	}

	// Ensure destination directory exists
	if err := CreateDir(filepath.Dir(dst)); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	// Copy file content
	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		dstFile.Close()
		return fmt.Errorf("failed to copy file content: %w", err)
	}
	// 显式 Close 并检查 error：写操作的 Close 可能 flush 缓冲并返回
	// 错误（如磁盘满/NFS 确认），defer 会吞掉这个 error 导致数据丢失。
	if err := dstFile.Close(); err != nil {
		return fmt.Errorf("failed to close destination file: %w", err)
	}

	// Preserve file permissions and modification time
	if err := os.Chmod(dst, srcInfo.Mode()); err != nil {
		return fmt.Errorf("failed to set file permissions: %w", err)
	}

	if err := os.Chtimes(dst, srcInfo.ModTime(), srcInfo.ModTime()); err != nil {
		return fmt.Errorf("failed to set file times: %w", err)
	}

	return nil
}

// MoveFile moves/renames a file from source to destination
func MoveFile(src, dst string) error {
	if err := CreateDir(filepath.Dir(dst)); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}
	return os.Rename(src, dst)
}

// TruncateFile truncates a file to the specified size (0 to empty the file)
func TruncateFile(fileName string, size int64) error {
	return os.Truncate(fileName, size)
}

// ReadRange seeks to start in rs and copies [start, end) bytes to w.
func ReadRange(rs io.ReadSeeker, w io.Writer, start, end int) error {
	if start < 0 {
		return fmt.Errorf("invalid start: %d", start)
	}
	if end < 0 {
		return fmt.Errorf("invalid end: %d", end)
	}
	if end < start {
		return fmt.Errorf("start (%d) must be <= end (%d)", start, end)
	}
	if _, err := rs.Seek(int64(start), io.SeekStart); err != nil {
		return err
	}
	remaining := int64(end - start)
	_, err := io.CopyN(w, rs, remaining)
	return err
}

// ========== 文件读写 ==========

// WriteMode defines how to write to a file
type WriteMode int

const (
	WriteModeCreate WriteMode = iota // Create new file or overwrite existing
	WriteModeAppend                  // Append to existing file or create new
)

// WriteFile writes data to a file with specified mode
func WriteFile(filename string, data []byte, mode WriteMode) error {
	return WriteFileFunc(filename, func(w *bufio.Writer) error {
		_, err := w.Write(data)
		return err
	}, mode)
}

// WriteString writes string to a file with specified mode
func WriteString(filename string, content string, mode WriteMode) error {
	return WriteFileFunc(filename, func(w *bufio.Writer) error {
		_, err := w.WriteString(content)
		return err
	}, mode)
}

// WriteFileFunc writes to a file using a custom writer function
func WriteFileFunc(filename string, writeFunc func(*bufio.Writer) error, mode WriteMode) error {
	// Create directory if needed
	if err := CreateDir(filepath.Dir(filename)); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Determine file flags
	var flags int
	switch mode {
	case WriteModeCreate:
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	case WriteModeAppend:
		flags = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	default:
		return fmt.Errorf("invalid write mode")
	}

	file, err := os.OpenFile(filename, flags, 0666)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	if err := writeFunc(writer); err != nil {
		return fmt.Errorf("write function failed: %w", err)
	}

	if err := writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush writer: %w", err)
	}

	return nil
}

// WriteFileExec writes data to filename with executable permissions (0755),
// creating parent directories as needed. It is the executable counterpart to
// WriteFile(path, data, WriteModeCreate): use it for shell scripts, binaries,
// or any artifact that must be marked executable on Unix.
//
// Unlike the legacy ioutil.WriteExeFile, this uses os.WriteFile (the modern
// replacement for the deprecated ioutil.WriteFile) and wraps errors with %w.
func WriteFileExec(filename string, data []byte) error {
	dir := filepath.Dir(filename)
	if err := CreateDir(dir); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	if err := os.WriteFile(filename, data, 0755); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	return nil
}

// WriteFromReader streams all data from r into filename, creating parent
// directories as needed. Returns the number of bytes written.
//
// Improvement over cor/disk.WriteIo: that impl used ioutil.ReadAll to buffer
// the entire reader into memory before writing. This version streams via
// io.Copy, so large readers (e.g. HTTP response bodies, multi-GB files)
// do not blow up the heap. The file is created with O_TRUNC so existing
// content is replaced.
func WriteFromReader(filename string, r io.Reader) (int64, error) {
	if err := CreateDir(filepath.Dir(filename)); err != nil {
		return 0, fmt.Errorf("failed to create directory: %w", err)
	}
	f, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return 0, fmt.Errorf("failed to create file: %w", err)
	}
	// 显式 Close 并检查 error：写操作的 Close 可能 flush 缓冲并返回
	// 错误（如磁盘满/NFS 确认），defer 会吞掉这个 error 导致数据丢失。
	n, copyErr := io.Copy(f, r)
	if cerr := f.Close(); cerr != nil {
		return n, fmt.Errorf("failed to close file: %w", cerr)
	}
	if copyErr != nil {
		return n, fmt.Errorf("failed to copy: %w", copyErr)
	}
	return n, nil
}

// ReadFile reads the entire content of a file
func ReadFile(filename string) ([]byte, error) {
	if !Exists(filename) {
		return nil, fmt.Errorf("file does not exist: %s", filename)
	}

	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	return data, nil
}

// ReadString reads the entire content of a file as string
func ReadString(filename string) (string, error) {
	data, err := ReadFile(filename)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ========== 路径操作 ==========

// JoinPath joins path elements using filepath.Join for cross-platform compatibility
func JoinPath(elements ...string) string {
	return filepath.Join(elements...)
}

// GetDir returns the directory portion of a path
func GetDir(path string) string {
	return filepath.Dir(path)
}

// GetFileName returns the filename with extension
func GetFileName(path string) string {
	return filepath.Base(path)
}

// GetFileNameWithoutExt returns the filename without extension
func GetFileNameWithoutExt(path string) string {
	name := filepath.Base(path)
	ext := filepath.Ext(name)
	return strings.TrimSuffix(name, ext)
}

// GetFileExt returns the file extension
func GetFileExt(path string) string {
	return filepath.Ext(path)
}

// ========== 目录遍历 ==========

// TraverseDirSlice recursively traverses directory and returns all file paths as slice
func TraverseDirSlice(dirPath string) ([]string, error) {
	if !IsDir(dirPath) {
		return nil, fmt.Errorf("path is not a directory: %s", dirPath)
	}

	var files []string
	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			files = append(files, path)
		}
		return nil
	})

	return files, err
}

// TraverseDirList recursively traverses directory and returns all file paths as list
func TraverseDirList(dirPath string) (*list.List, error) {
	files, err := TraverseDirSlice(dirPath)
	if err != nil {
		return nil, err
	}

	fileList := list.New()
	for _, file := range files {
		fileList.PushBack(file)
	}
	return fileList, nil
}

// WalkFiles walks through directory and returns files matching the suffix
func WalkFiles(dir, suffix string, includeDirs bool) ([]string, error) {
	var results []string
	suffix = strings.ToUpper(suffix)

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Filter by type
		if includeDirs && !info.IsDir() {
			return nil
		}
		if !includeDirs && info.IsDir() {
			return nil
		}

		// Filter by suffix
		if suffix == "" || strings.HasSuffix(strings.ToUpper(info.Name()), suffix) {
			results = append(results, path)
		}
		return nil
	})

	return results, err
}

// ========== 哈希计算 ==========
//
// 文件内容哈希与文件读写强相关（打开+读取+计算），配套放在文件操作
// 一起，避免散落多处。CalcIOHash 虽接受 io.Reader，但与文件哈希同源。

// CalcSHA1 calculates SHA1 hash of a file
func CalcSHA1(filePath string) (string, error) {
	return calcFileHash(filePath, sha1.New())
}

// CalcSHA256 calculates SHA256 hash of a file
func CalcSHA256(filePath string) (string, error) {
	return calcFileHash(filePath, sha256.New())
}

// calcFileHash calculates hash of a file using the provided hasher
func calcFileHash(filePath string, hasher io.Writer) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf("failed to calculate hash: %w", err)
	}

	// Type assertion to get Sum method
	h := hasher.(interface{ Sum([]byte) []byte })
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// CalcIOHash calculates hash from an io.Reader
func CalcIOHash(reader io.Reader, useSHA256 bool) (string, error) {
	var hasher io.Writer
	if useSHA256 {
		hasher = sha256.New()
	} else {
		hasher = sha1.New()
	}

	if _, err := io.Copy(hasher, reader); err != nil {
		return "", fmt.Errorf("failed to calculate hash: %w", err)
	}

	h := hasher.(interface{ Sum([]byte) []byte })
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// ========== 路径安全 ==========

// ErrPathEmpty is returned by SafeJoin when the given path is empty.
var ErrPathEmpty = errors.New("path is empty")

// ErrPathAbsolute is returned by SafeJoin when the given path is absolute.
var ErrPathAbsolute = errors.New("absolute path not allowed")

// ErrPathEscapesCWD is returned by SafeJoin when the joined path escapes cwd.
var ErrPathEscapesCWD = errors.New("path escapes cwd")

// SafeJoin joins cwd and path, ensuring the result does not escape cwd.
// It rejects empty paths, absolute paths, and traversal (../../) attempts.
// cwd is used as the base; path must be a relative path that stays within cwd.
//
// Examples:
//
//	SafeJoin("/a/b", "c/d")     // "/a/b/c/d", nil
//	SafeJoin("/a/b", "")        // "", ErrPathEmpty
//	SafeJoin("/a/b", "/c/d")    // "", ErrPathAbsolute
//	SafeJoin("/a/b", "../../c") // "", ErrPathEscapesCWD
func SafeJoin(cwd, path string) (string, error) {
	if path == "" {
		return "", ErrPathEmpty
	}
	cleaned := filepath.Clean(path)
	if filepath.IsAbs(cleaned) {
		return "", ErrPathAbsolute
	}
	joined := filepath.Join(cwd, cleaned)
	rel, err := filepath.Rel(cwd, joined)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrPathEscapesCWD
	}
	return joined, nil
}

// Contains reports whether child is inside parent (or equal to parent).
// Both paths are Cleaned and made absolute before comparison.
// Returns false if either path is empty or cannot be resolved.
//
// Examples:
//
//	Contains("/a/b", "/a/b/c")   // true
//	Contains("/a/b", "/a/b")     // true
//	Contains("/a/b", "/a/bc")    // false (boundary-safe, no prefix false positive)
//	Contains("/a/b", "/a/../a/b/c") // true (Cleaned)
func Contains(parent, child string) bool {
	if parent == "" || child == "" {
		return false
	}
	pAbs, err := filepath.Abs(filepath.Clean(parent))
	if err != nil {
		return false
	}
	cAbs, err := filepath.Abs(filepath.Clean(child))
	if err != nil {
		return false
	}
	sep := string(filepath.Separator)
	return cAbs == pAbs || strings.HasPrefix(cAbs+sep, pAbs+sep)
}

// Overlaps reports whether a and b share any common directory prefix.
// Returns true if a Contains b or b Contains a.
// Returns false if either path is empty or cannot be resolved.
//
// Examples:
//
//	Overlaps("/a/b", "/a/b/c")    // true
//	Overlaps("/a/b/c", "/a/b")    // true
//	Overlaps("/a/b", "/a/b")      // true
//	Overlaps("/a/b", "/x/y")      // false
func Overlaps(a, b string) bool {
	return Contains(a, b) || Contains(b, a)
}
