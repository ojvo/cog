package env

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
)

// ========== 文件比较 ==========
//
// cor/disk 的 Compare/CompareTxt 返回 bool/int 魔法值，错误被吞掉；
// 此处重构为 (bool, error) 语义：true+nil = 完全相同；false+nil = 不相同；
// false+err = 无法比较（如文件不存在、读取失败）。调用者可显式区分
// "不同" 与 "出错"，避免把 I/O 错误误判为内容差异。

// CompareFiles reports whether src and dst have identical byte content.
// If checkModTime is true, the modification times must also match (Equal)
// before content is compared.
//
// Returns:
//   - (true, nil)  if files are identical
//   - (false, nil) if files differ in size, modtime, or content
//   - (false, err) if either file cannot be stat'd or opened
//
// Improvements over cor/disk.Compare:
//   - Original returned false for all error cases (stat fail, open fail,
//     content differ), making it impossible to distinguish "different" from
//     "missing". This version surfaces errors.
//   - Original comparebyte compared the full 512-byte buffer rather than
//     the n bytes actually read. While the stale-tail bug is masked by the
//     "size verified + early return on first difference" invariant, the
//     implementation was fragile. This version compares only sBuf[:sN] and
//     uses larger 4KB buffers for throughput.
//   - Original called comparefile which opened files without surfacing
//     open errors. This version propagates them.
func CompareFiles(src, dst string, checkModTime bool) (bool, error) {
	sinfo, err := os.Lstat(src)
	if err != nil {
		return false, fmt.Errorf("failed to stat source %s: %w", src, err)
	}
	dinfo, err := os.Lstat(dst)
	if err != nil {
		return false, fmt.Errorf("failed to stat destination %s: %w", dst, err)
	}
	if sinfo.Size() != dinfo.Size() {
		return false, nil
	}
	if checkModTime && !sinfo.ModTime().Equal(dinfo.ModTime()) {
		return false, nil
	}

	sFile, err := os.Open(src)
	if err != nil {
		return false, fmt.Errorf("failed to open source %s: %w", src, err)
	}
	defer sFile.Close()

	dFile, err := os.Open(dst)
	if err != nil {
		return false, fmt.Errorf("failed to open destination %s: %w", dst, err)
	}
	defer dFile.Close()

	// Stream-compare in 4KB chunks. Only the n bytes actually read are
	// compared (sBuf[:sN] vs dBuf[:dN]); stale tail bytes from prior
	// iterations are excluded.
	const bufSize = 4096
	sBuf := make([]byte, bufSize)
	dBuf := make([]byte, bufSize)
	sReader := bufio.NewReaderSize(sFile, bufSize)
	dReader := bufio.NewReaderSize(dFile, bufSize)
	for {
		sN, sErr := sReader.Read(sBuf)
		dN, dErr := dReader.Read(dBuf)

		// Length mismatch or content mismatch → not equal
		if sN != dN || !bytes.Equal(sBuf[:sN], dBuf[:dN]) {
			return false, nil
		}

		// Both files were verified to have equal size, so EOF should
		// arrive simultaneously. If one reaches EOF before the other
		// (e.g. file truncated between Lstat and Read), treat as not
		// equal — no error to surface, since the size check passed
		// earlier and the caller expects a boolean answer.
		if sErr != nil || dErr != nil {
			if sErr == io.EOF && dErr == io.EOF {
				return true, nil
			}
			if sErr != nil && sErr != io.EOF {
				return false, fmt.Errorf("source read error: %w", sErr)
			}
			if dErr != nil && dErr != io.EOF {
				return false, fmt.Errorf("destination read error: %w", dErr)
			}
			// One EOF, other still nil — sizes diverged at runtime.
			return false, nil
		}
	}
}

// CompareText reports whether src and dst have identical text content.
// If ignoreLineEndings is true, "\r\n" is normalized to "\n" and a single
// trailing "\n" is stripped from each side before comparison — so a file
// ending in "\r\n" compares equal to one ending in "\n" or with no trailing
// newline.
//
// Returns:
//   - (true, nil)  if text content matches
//   - (false, nil) if content differs in length or bytes
//   - (false, err) if either file cannot be read
//
// Improvement over cor/disk.CompareTxt: the original returned magic int
// codes (0=equal, 1=src err, 2=dst err, 3=length differs, 4=content
// differs) which callers had to decode. This version returns (bool, error).
// It also uses the env.ReadString helper rather than cor/disk.Read's
// manual loop, and exposes the line-ending normalization as a bool flag
// rather than the BpIgnoreEnter = 1 sentinel constant.
//
// Note: Both files are read fully into memory. For very large files
// prefer CompareFiles (byte-level streaming, no full load).
func CompareText(src, dst string, ignoreLineEndings bool) (bool, error) {
	srcText, err := ReadString(src)
	if err != nil {
		return false, fmt.Errorf("failed to read source %s: %w", src, err)
	}
	dstText, err := ReadString(dst)
	if err != nil {
		return false, fmt.Errorf("failed to read destination %s: %w", dst, err)
	}
	if ignoreLineEndings {
		srcText = strings.ReplaceAll(srcText, "\r\n", "\n")
		dstText = strings.ReplaceAll(dstText, "\r\n", "\n")
		srcText = strings.TrimSuffix(srcText, "\n")
		dstText = strings.TrimSuffix(dstText, "\n")
	}
	if len(srcText) != len(dstText) {
		return false, nil
	}
	return srcText == dstText, nil
}
