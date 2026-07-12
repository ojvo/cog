package store

import (
	"bytes"
	"compress/zlib"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrKeyNotFound = errors.New("key not found")
	ErrKeyExpired  = errors.New("key expired")
)

// 记录头信息
// 修改记录头信息结构体
type recordHeader struct {
	CRC       uint32 // 改为大写
	Timestamp uint32
	KeySize   uint32
	ValueSize uint32
	ExpireAt  uint32
}

// 带过期时间的值结构
type valueWithTTL struct {
	Value      interface{} `json:"value"`
	ExpireAt   int64       `json:"expire_at,omitempty"`
	CreatedAt  int64       `json:"created_at"`
	LastAccess int64       `json:"last_access,omitempty"`
}

// DbLite 基于Bitcask模型重构的键值数据库
type DbLite struct {
	mu             sync.RWMutex
	activeFile     *os.File
	activeFileID   uint32
	dataDir        string
	keyDir         map[string]KeyDir
	dataFiles      map[uint32]*os.File
	fileMu         sync.RWMutex
	mergeRunning   atomic.Bool
	useCompression bool
	encryptionKey  []byte
	stats          *DBStats
	stopChan       chan struct{}
	stopped        bool
	wg             sync.WaitGroup
}

// KeyDir 内存中的键目录
type KeyDir struct {
	FileID    uint32
	Offset    int64
	Size      int32
	Timestamp int64
	ExpireAt  int64
}

// DBStats 数据库统计信息
type DBStats struct {
	Reads     int64     `json:"reads"`
	Writes    int64     `json:"writes"`
	Deletes   int64     `json:"deletes"`
	LastSync  time.Time `json:"last_sync"`
	KeyCount  int       `json:"key_count"`
	FileCount int       `json:"file_count"`
	mu        sync.Mutex
}

// DBOption 数据库选项函数类型
type DBOption func(*DbLite)

// NewDbLite 创建新的DbLite实例
func NewDbLite(dataDir string, options ...DBOption) (*DbLite, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %v", err)
	}

	db := &DbLite{
		dataDir:   dataDir,
		keyDir:    make(map[string]KeyDir),
		dataFiles: make(map[uint32]*os.File),
		stats:     &DBStats{LastSync: time.Now()},
		stopChan:  make(chan struct{}),
	}

	for _, option := range options {
		option(db)
	}

	// 加载现有数据文件
	if err := db.loadDataFiles(); err != nil {
		return nil, fmt.Errorf("加载数据文件失败: %v", err)
	}

	// 创建或切换到新的活跃文件
	useLastFile := true
	if useLastFile {
		if err := db.reuseLastActiveFile(); err != nil {
			return nil, err
		}
	} else {
		if err := db.rotateActiveFile(); err != nil {
			return nil, fmt.Errorf("初始化活跃文件失败: %v", err)
		}
	}

	// 启动后台合并协程
	db.wg.Add(1)
	go db.startMergeWorker()

	return db, nil
}

// WithEncryption 启用加密
func WithEncryption(key []byte) DBOption {
	return func(db *DbLite) {
		switch len(key) {
		case 16, 24, 32:
			db.encryptionKey = key
		default:
			hashedKey := sha1.Sum(key)
			db.encryptionKey = hashedKey[:16] // 使用AES-128
		}
	}
}

// WithCompression 启用压缩
func WithCompression() DBOption {
	return func(db *DbLite) {
		db.useCompression = true
	}
}

// 加载现有数据文件
func (db *DbLite) loadDataFiles() error {
	files, err := os.ReadDir(db.dataDir)
	if err != nil {
		return err
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		var fileID uint32
		if _, err := fmt.Sscanf(file.Name(), "%d.data", &fileID); err != nil {
			continue
		}

		f, err := os.OpenFile(filepath.Join(db.dataDir, file.Name()), os.O_RDONLY, 0644)
		if err != nil {
			return err
		}

		db.dataFiles[fileID] = f

		// 构建内存索引
		if err := db.buildKeyDir(f, fileID); err != nil {
			f.Close()
			return err
		}

		// 更新最大文件ID
		if fileID >= db.activeFileID {
			db.activeFileID = fileID + 1
		}
	}

	db.stats.mu.Lock()
	db.stats.FileCount = len(db.dataFiles)
	db.stats.KeyCount = len(db.keyDir)
	db.stats.mu.Unlock()

	return nil
}

// 构建内存索引
func (db *DbLite) buildKeyDir(file *os.File, fileID uint32) error {
	var offset int64 = 0

	for {
		header, err := readRecordHeader(file)
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		key := make([]byte, header.KeySize)
		if _, err := io.ReadFull(file, key); err != nil {
			return err
		}

		// 跳过值数据
		if _, err := file.Seek(int64(header.ValueSize), io.SeekCurrent); err != nil {
			return err
		}

		keyStr := string(key)
		db.keyDir[keyStr] = KeyDir{
			FileID:    fileID,
			Offset:    offset,
			Size:      int32(binary.Size(header)) + int32(header.KeySize+header.ValueSize),
			Timestamp: int64(header.Timestamp),
			ExpireAt:  int64(header.ExpireAt),
		}

		offset, _ = file.Seek(0, io.SeekCurrent)
	}

	return nil
}

// rotateActiveFile switches to a new active file. Acquires fileMu.
func (db *DbLite) rotateActiveFile() error {
	db.fileMu.Lock()
	defer db.fileMu.Unlock()
	return db.rotateActiveFileLocked()
}

// rotateActiveFileLocked creates a new active file. Caller must hold fileMu.
func (db *DbLite) rotateActiveFileLocked() error {
	if db.activeFile != nil {
		db.activeFile.Close()
	}

	db.activeFileID++

	filePath := filepath.Join(db.dataDir, fmt.Sprintf("%d.data", db.activeFileID))
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return err
	}

	db.activeFile = f
	db.dataFiles[db.activeFileID] = f

	db.stats.mu.Lock()
	db.stats.FileCount++
	db.stats.mu.Unlock()

	return nil
}

func (db *DbLite) reuseLastActiveFile() error {
	if len(db.dataFiles) == 0 {
		return db.rotateActiveFile()
	}

	// 找到ID最大的文件
	var maxID uint32
	for id := range db.dataFiles {
		if id > maxID {
			maxID = id
		}
	}

	// 设置为当前活跃文件
	filePath := filepath.Join(db.dataDir, fmt.Sprintf("%d.data", maxID))
	f, err := os.OpenFile(filePath, os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return err
	}

	db.activeFile = f
	db.activeFileID = maxID
	return nil
}

// Set 设置键值对
func (db *DbLite) Set(key string, value interface{}) error {
	return db.SetWithTTL(key, value, 0)
}

// SetWithTTL 设置带过期时间的键值对
func (db *DbLite) SetWithTTL(key string, value interface{}, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}

	var expireAt int64 = 0
	if ttl > 0 {
		expireAt = time.Now().Add(ttl).Unix()
	}

	// 压缩和加密
	if db.useCompression {
		data, err = compress(data)
		if err != nil {
			return err
		}
	}

	if db.encryptionKey != nil {
		data, err = encrypt(data, db.encryptionKey)
		if err != nil {
			return err
		}
	}

	// 写入记录
	offset, size, err := db.appendRecord(key, data, expireAt)
	if err != nil {
		return err
	}

	// 更新内存索引
	db.mu.Lock()
	db.keyDir[key] = KeyDir{
		FileID:    db.activeFileID,
		Offset:    offset,
		Size:      size,
		Timestamp: time.Now().UnixNano(),
		ExpireAt:  expireAt,
	}
	db.mu.Unlock()

	db.stats.mu.Lock()
	db.stats.Writes++
	db.stats.KeyCount = len(db.keyDir)
	db.stats.mu.Unlock()

	return nil
}

// Close 关闭数据库
func (db *DbLite) Close() error {
	db.mu.Lock()
	if db.stopped {
		db.mu.Unlock()
		return nil
	}
	db.stopped = true
	close(db.stopChan)
	db.mu.Unlock()

	// Wait for merge worker to exit before closing files
	db.wg.Wait()

	db.fileMu.Lock()
	defer db.fileMu.Unlock()

	var firstErr error
	for _, f := range db.dataFiles {
		if err := f.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	// 清空 map
	db.dataFiles = make(map[uint32]*os.File)
	db.activeFile = nil

	return firstErr
}

// 追加记录到活跃文件
// 在DbLite结构体中添加常量
const (
	maxFileSize  = 100 << 20 // 100MB单个文件最大尺寸
	syncInterval = 100       // 每100次写入同步一次
)

// 修改appendRecord方法
func (db *DbLite) appendRecord(key string, value []byte, expireAt int64) (offset int64, size int32, err error) {
	db.fileMu.Lock()
	defer db.fileMu.Unlock()

	// 检查当前文件大小
	if stat, err := db.activeFile.Stat(); err == nil && stat.Size() > maxFileSize {
		if err := db.rotateActiveFileLocked(); err != nil {
			return 0, 0, fmt.Errorf("文件切换失败: %v", err)
		}
	}

	offset, err = db.activeFile.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, 0, err
	}

	header := recordHeader{
		Timestamp: uint32(time.Now().Unix()),
		KeySize:   uint32(len(key)),
		ValueSize: uint32(len(value)),
		ExpireAt:  uint32(expireAt),
	}

	// 使用缓冲写入提高性能
	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.LittleEndian, header); err != nil {
		return 0, 0, err
	}
	if _, err := buf.WriteString(key); err != nil {
		return 0, 0, err
	}
	if _, err := buf.Write(value); err != nil {
		return 0, 0, err
	}

	// 计算CRC并更新header
	header.CRC = crc32(buf.Bytes())
	if _, err := db.activeFile.Seek(offset, io.SeekStart); err != nil {
		return 0, 0, err
	}
	if err := binary.Write(db.activeFile, binary.LittleEndian, header); err != nil {
		return 0, 0, err
	}

	// 写入实际数据
	if _, err := db.activeFile.Write(buf.Bytes()[binary.Size(header):]); err != nil {
		return 0, 0, err
	}

	// 定期同步到磁盘
	if db.stats.Writes%syncInterval == 0 {
		if err := db.activeFile.Sync(); err != nil {
			return 0, 0, err
		}
	}

	size = int32(binary.Size(header)) + int32(len(key)+len(value))
	return offset, size, nil
}

func (db *DbLite) GetKeys() ([]string, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	keys := make([]string, 0, len(db.keyDir))
	for key := range db.keyDir {
		keys = append(keys, key)
	}
	return keys, nil
}

// Get 获取键值对
func (db *DbLite) Get(key string, value interface{}) error {
	db.stats.mu.Lock()
	db.stats.Reads++
	db.stats.mu.Unlock()

	// 持有 RLock 直到 readRecord 完成，防止 Merge 并发关闭文件
	db.mu.RLock()
	defer db.mu.RUnlock()

	dir, exists := db.keyDir[key]
	if !exists {
		return ErrKeyNotFound
	}

	// 检查过期（不在此处启动异步删除，避免与并发 Set 产生竞态；过期键由 Merge 清理）
	if dir.ExpireAt > 0 && dir.ExpireAt < time.Now().Unix() {
		return ErrKeyExpired
	}

	// 从文件读取数据
	data, err := db.readRecord(dir)
	if err != nil {
		return err
	}

	// 解密和解压
	if db.encryptionKey != nil {
		data, err = decrypt(data, db.encryptionKey)
		if err != nil {
			return err
		}
	}

	if db.useCompression {
		data, err = decompress(data)
		if err != nil {
			return err
		}
	}

	return json.Unmarshal(data, value)
}

// 从文件读取记录
// 在指定位置读取记录头
func readRecordHeaderAt(file *os.File, offset int64) (*recordHeader, error) {
	// 使用 SectionReader 避免并发 Seek 问题
	// 假设 Header 大小固定或有一个最大值，这里先读取固定大小的 Header 结构
	headerSize := binary.Size(recordHeader{})
	sr := io.NewSectionReader(file, offset, int64(headerSize))

	var header recordHeader
	if err := binary.Read(sr, binary.LittleEndian, &header); err != nil {
		return nil, err
	}
	return &header, nil
}

// 修改readRecord方法中的调用
func (db *DbLite) readRecord(dir KeyDir) ([]byte, error) {
	db.fileMu.RLock()
	file, exists := db.dataFiles[dir.FileID]
	db.fileMu.RUnlock()

	if !exists {
		return nil, ErrKeyNotFound
	}

	// 读取记录头
	header, err := readRecordHeaderAt(file, dir.Offset)
	if err != nil {
		return nil, err
	}

	// 计算值的位置
	valueOffset := dir.Offset + int64(binary.Size(header)) + int64(header.KeySize)

	// 使用 SectionReader 读取值
	value := make([]byte, header.ValueSize)
	sr := io.NewSectionReader(file, valueOffset, int64(header.ValueSize))
	if _, err := io.ReadFull(sr, value); err != nil {
		return nil, err
	}

	return value, nil
}

// Delete 删除键值对
func (db *DbLite) Delete(key string) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if _, exists := db.keyDir[key]; !exists {
		return ErrKeyNotFound
	}

	// 写入墓碑记录
	if _, _, err := db.appendRecord(key, []byte{}, 1); err != nil {
		return err
	}

	// 从内存索引删除
	delete(db.keyDir, key)

	db.stats.mu.Lock()
	db.stats.Deletes++
	db.stats.KeyCount = len(db.keyDir)
	db.stats.mu.Unlock()

	return nil
}

// 启动合并工作协程
func (db *DbLite) startMergeWorker() {
	defer db.wg.Done()

	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			db.Merge()
		case <-db.stopChan:
			return
		}
	}
}

// Merge 合并数据文件
func (db *DbLite) Merge() error {
	if !db.mergeRunning.CompareAndSwap(false, true) {
		return nil
	}
	defer db.mergeRunning.Store(false)

	// 创建新的合并文件
	newFileID := db.activeFileID + 1
	newFile, err := os.OpenFile(
		filepath.Join(db.dataDir, fmt.Sprintf("%d.data", newFileID)),
		os.O_CREATE|os.O_RDWR,
		0644,
	)
	if err != nil {
		return err
	}

	// 遍历内存索引，写入有效数据
	newKeyDir := make(map[string]KeyDir)
	offset := int64(0)

	db.mu.RLock()
	for key, dir := range db.keyDir {
		// 跳过已删除或过期的记录
		if dir.ExpireAt > 0 && dir.ExpireAt < time.Now().Unix() {
			continue
		}

		// 读取原始记录
		data, err := db.readRecord(dir)
		if err != nil {
			continue
		}

		// 写入新文件
		size, err := writeRecord(newFile, key, data, dir.ExpireAt)
		if err != nil {
			continue
		}

		// 更新索引
		newKeyDir[key] = KeyDir{
			FileID:    newFileID,
			Offset:    offset,
			Size:      size,
			Timestamp: dir.Timestamp,
			ExpireAt:  dir.ExpireAt,
		}

		offset += int64(size)
	}
	db.mu.RUnlock()

	// 原子切换：同时持有 db.mu 和 fileMu，保证 keyDir 与 dataFiles 一致性
	// 锁顺序 db.mu → fileMu 与 Delete 一致，避免死锁
	db.mu.Lock()
	db.fileMu.Lock()
	db.keyDir = newKeyDir
	db.activeFileID = newFileID
	db.dataFiles[newFileID] = newFile
	// 清理旧文件（merged 文件 newFileID 不受影响）
	for id, file := range db.dataFiles {
		if id < newFileID {
			file.Close()
			delete(db.dataFiles, id)
			os.Remove(filepath.Join(db.dataDir, fmt.Sprintf("%d.data", id)))
		}
	}
	// 旧 activeFile 已在上方清理中关闭，置 nil 避免 rotateActiveFile 双重 Close
	db.activeFile = nil
	db.fileMu.Unlock()
	db.mu.Unlock()

	// 切换到新活跃文件（merged 文件作为只读数据文件保留）
	if err := db.rotateActiveFile(); err != nil {
		return err
	}

	db.stats.mu.Lock()
	db.stats.FileCount = len(db.dataFiles)
	db.stats.KeyCount = len(db.keyDir)
	db.stats.mu.Unlock()

	return nil
}

// (Duplicate Close method removed)

// 读取记录头
// 修改readRecordHeader方法
func readRecordHeader(file *os.File) (*recordHeader, error) {
	header := &recordHeader{}
	if err := binary.Read(file, binary.LittleEndian, header); err != nil {
		return nil, err
	}
	return header, nil
}

// 修改writeRecord方法
func writeRecord(file *os.File, key string, value []byte, expireAt int64) (int32, error) {
	header := recordHeader{
		Timestamp: uint32(time.Now().Unix()),
		KeySize:   uint32(len(key)),
		ValueSize: uint32(len(value)),
		ExpireAt:  uint32(expireAt),
	}

	// 计算CRC校验和
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, header)
	buf.WriteString(key)
	buf.Write(value)
	header.CRC = crc32(buf.Bytes())

	// 写入记录
	if err := binary.Write(file, binary.LittleEndian, header); err != nil {
		return 0, err
	}
	if _, err := file.WriteString(key); err != nil {
		return 0, err
	}
	if _, err := file.Write(value); err != nil {
		return 0, err
	}

	size := int32(binary.Size(header)) + int32(len(key)+len(value))
	return size, nil
}

// CRC32校验和
func crc32(data []byte) uint32 {
	var crc uint32 = 0xffffffff
	for _, b := range data {
		crc ^= uint32(b)
		for i := 0; i < 8; i++ {
			if crc&1 == 1 {
				crc = (crc >> 1) ^ 0xedb88320
			} else {
				crc >>= 1
			}
		}
	}
	return crc ^ 0xffffffff
}

// 压缩数据
func compress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// 解压数据
func decompress(data []byte) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

// 加密数据
func encrypt(data, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	ciphertext := make([]byte, aes.BlockSize+len(data))
	iv := ciphertext[:aes.BlockSize]
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, err
	}

	stream := cipher.NewCFBEncrypter(block, iv)
	stream.XORKeyStream(ciphertext[aes.BlockSize:], data)

	return ciphertext, nil
}

// 解密数据
func decrypt(ciphertext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	if len(ciphertext) < aes.BlockSize {
		return nil, errors.New("密文太短")
	}

	iv := ciphertext[:aes.BlockSize]
	ciphertext = ciphertext[aes.BlockSize:]

	stream := cipher.NewCFBDecrypter(block, iv)
	stream.XORKeyStream(ciphertext, ciphertext)

	return ciphertext, nil
}
