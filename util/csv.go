package util

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// CSVWriter 提供线程安全的CSV文件写入功能
type CSVWriter struct {
	filePath string
	headers  []string
	mutex    sync.Mutex
}

// NewCSVWriter 创建一个新的CSV写入器
func NewCSVWriter(filePath string, headers []string) (*CSVWriter, error) {
	// 确保目录存在
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("创建目录失败: %v", err)
	}

	writer := &CSVWriter{
		filePath: filePath,
		headers:  headers,
	}

	// 检查文件是否存在
	_, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		// 创建文件并写入表头
		if err := writer.writeHeaders(); err != nil {
			return nil, err
		}
	}

	return writer, nil
}

// writeHeaders 写入CSV表头
func (w *CSVWriter) writeHeaders() error {
	file, err := os.Create(w.filePath)
	if err != nil {
		return fmt.Errorf("创建CSV文件失败: %v", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	if err := writer.Write(w.headers); err != nil {
		return fmt.Errorf("写入CSV表头失败: %v", err)
	}

	return nil
}

// AppendRow 向CSV文件追加一行数据
func (w *CSVWriter) AppendRow(row []string) error {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	// 打开文件用于追加
	file, err := os.OpenFile(w.filePath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		// 如果文件不存在，创建文件并写入表头
		if os.IsNotExist(err) {
			if err := w.writeHeaders(); err != nil {
				return err
			}
			file, err = os.OpenFile(w.filePath, os.O_APPEND|os.O_WRONLY, 0644)
			if err != nil {
				return fmt.Errorf("打开CSV文件失败: %v", err)
			}
		} else {
			return fmt.Errorf("打开CSV文件失败: %v", err)
		}
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	if err := writer.Write(row); err != nil {
		return fmt.Errorf("写入CSV行失败: %v", err)
	}

	return nil
}

// ReadAll 读取CSV文件的所有行
func (w *CSVWriter) ReadAll() ([][]string, error) {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	// 检查文件是否存在
	_, err := os.Stat(w.filePath)
	if os.IsNotExist(err) {
		return [][]string{w.headers}, nil
	}

	// 打开文件
	file, err := os.Open(w.filePath)
	if err != nil {
		return nil, fmt.Errorf("打开CSV文件失败: %v", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("读取CSV文件失败: %v", err)
	}

	return records, nil
}

// CSVReader 提供CSV文件读取功能
type CSVReader struct {
	filePath string
	headers  []string
	mutex    sync.RWMutex
}

// NewCSVReader 创建一个新的CSV读取器
func NewCSVReader(filePath string) (*CSVReader, error) {
	reader := &CSVReader{
		filePath: filePath,
	}

	// 检查文件是否存在
	_, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("CSV文件不存在: %s", filePath)
	}

	// 读取表头
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("打开CSV文件失败: %v", err)
	}
	defer file.Close()

	csvReader := csv.NewReader(file)
	headers, err := csvReader.Read()
	if err != nil {
		return nil, fmt.Errorf("读取CSV表头失败: %v", err)
	}

	reader.headers = headers
	return reader, nil
}

// GetHeaders 获取CSV文件的表头
func (r *CSVReader) GetHeaders() []string {
	return r.headers
}

// ReadRows 读取CSV文件的所有数据行（不包括表头）
func (r *CSVReader) ReadRows() ([][]string, error) {
	r.mutex.RLock()
	defer r.mutex.RUnlock()

	// 打开文件
	file, err := os.Open(r.filePath)
	if err != nil {
		return nil, fmt.Errorf("打开CSV文件失败: %v", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	
	// 读取所有记录
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("读取CSV文件失败: %v", err)
	}

	// 如果只有表头或者没有数据，返回空切片
	if len(records) <= 1 {
		return [][]string{}, nil
	}

	// 返回数据行（不包括表头）
	return records[1:], nil
}

// GetFilePath 返回CSV文件的路径
func (w *CSVWriter) GetFilePath() string {
    return w.filePath
}

// UpdateRow 根据指定的键列和值更新CSV中的行
func (w *CSVWriter) UpdateRow(keyColumn int, keyValue string, newRow []string) error {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	// 检查文件是否存在
	_, err := os.Stat(w.filePath)
	if os.IsNotExist(err) {
		// 如果文件不存在，创建文件并写入表头和新行
		if err := w.writeHeaders(); err != nil {
			return err
		}
		file, err := os.OpenFile(w.filePath, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("打开CSV文件失败: %v", err)
		}
		defer file.Close()

		writer := csv.NewWriter(file)
		defer writer.Flush()

		if err := writer.Write(newRow); err != nil {
			return fmt.Errorf("写入CSV行失败: %v", err)
		}
		return nil
	}

	// 读取所有记录
	file, err := os.Open(w.filePath)
	if err != nil {
		return fmt.Errorf("打开CSV文件失败: %v", err)
	}
	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	file.Close()

	if err != nil {
		return fmt.Errorf("读取CSV文件失败: %v", err)
	}

	// 如果没有记录，只有表头，直接添加新行
	if len(records) <= 1 {
		file, err = os.OpenFile(w.filePath, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("打开CSV文件失败: %v", err)
		}
		defer file.Close()

		writer := csv.NewWriter(file)
		defer writer.Flush()

		if err := writer.Write(newRow); err != nil {
			return fmt.Errorf("写入CSV行失败: %v", err)
		}
		return nil
	}

	// 查找并更新行
	found := false
	for i := 1; i < len(records); i++ {
		if keyColumn < len(records[i]) && records[i][keyColumn] == keyValue {
			records[i] = newRow
			found = true
			break
		}
	}

	// 如果没找到匹配的行，添加新行
	if !found {
		records = append(records, newRow)
	}

	// 重写整个文件
	file, err = os.Create(w.filePath)
	if err != nil {
		return fmt.Errorf("创建CSV文件失败: %v", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	for _, record := range records {
		if err := writer.Write(record); err != nil {
			return fmt.Errorf("写入CSV行失败: %v", err)
		}
	}

	return nil
}