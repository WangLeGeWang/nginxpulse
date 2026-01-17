package ingest

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/likaia/nginxpulse/internal/config"
	"github.com/likaia/nginxpulse/internal/enrich"
	"github.com/likaia/nginxpulse/internal/store"
	"github.com/sirupsen/logrus"
)

var (
	nginxLogPattern = regexp.MustCompile(`^(\S+) - (\S+) \[([^\]]+)\] "(\S+) ([^"]+) HTTP\/\d\.\d" (\d+) (\d+) "([^"]*)" "([^"]*)"`)
	lastCleanupDate = ""
	ipParsingMu     sync.RWMutex
	ipParsing       bool
)

var ErrParsingInProgress = errors.New("日志解析中，请稍后重试")

// 解析结果
type ParserResult struct {
	WebName      string
	WebID        string
	TotalEntries int
	Duration     time.Duration
	Success      bool
	Error        error
}

type LogScanState struct {
	Files map[string]FileState `json:"files"` // 每个文件的状态
}

type FileState struct {
	LastOffset int64 `json:"last_offset"`
	LastSize   int64 `json:"last_size"`
}

type LogParser struct {
	repo          *store.Repository
	statePath     string
	states        map[string]LogScanState // 各网站的扫描状态，以网站ID为键
	demoMode      bool
	retentionDays int
}

// NewLogParser 创建新的日志解析器
func NewLogParser(userRepoPtr *store.Repository) *LogParser {
	statePath := filepath.Join(config.DataDir, "nginx_scan_state.json")
	cfg := config.ReadConfig()
	retentionDays := cfg.System.LogRetentionDays
	if retentionDays <= 0 {
		retentionDays = 30
	}
	parser := &LogParser{
		repo:          userRepoPtr,
		statePath:     statePath,
		states:        make(map[string]LogScanState),
		demoMode:      cfg.System.DemoMode,
		retentionDays: retentionDays,
	}
	parser.loadState()
	enrich.InitPVFilters()
	return parser
}

// loadState 加载上次扫描状态
func (p *LogParser) loadState() {
	data, err := os.ReadFile(p.statePath)
	if os.IsNotExist(err) {
		// 状态文件不存在，创建空状态映射
		p.states = make(map[string]LogScanState)
		return
	}

	if err != nil {
		logrus.Errorf("无法读取扫描状态文件: %v", err)
		p.states = make(map[string]LogScanState)
		return
	}

	if err := json.Unmarshal(data, &p.states); err != nil {
		logrus.Errorf("解析扫描状态失败: %v", err)
		p.states = make(map[string]LogScanState)
	}
}

// updateState 更新并保存状态
func (p *LogParser) updateState() {
	data, err := json.Marshal(p.states)
	if err != nil {
		logrus.Errorf("保存扫描状态失败: %v", err)
		return
	}

	if err := os.WriteFile(p.statePath, data, 0644); err != nil {
		logrus.Errorf("保存扫描状态失败: %v", err)
	}
}

// CleanOldLogs 清理保留天数之前的日志数据
func (p *LogParser) CleanOldLogs() error {
	today := time.Now().Format("2006-01-02")
	currentHour := time.Now().Hour()

	shouldClean := lastCleanupDate == "" || (currentHour == 2 && lastCleanupDate != today)

	if !shouldClean {
		return nil
	}

	err := p.repo.CleanOldLogs()
	if err != nil {
		return err
	}

	lastCleanupDate = today

	return nil
}

// ScanNginxLogs 增量扫描Nginx日志文件
func (p *LogParser) ScanNginxLogs() []ParserResult {
	if p.demoMode {
		return []ParserResult{}
	}
	if !startIPParsing() {
		return []ParserResult{}
	}
	defer finishIPParsing()

	websiteIDs := config.GetAllWebsiteIDs()
	return p.scanNginxLogsInternal(websiteIDs)
}

// ScanNginxLogsForWebsite 扫描指定网站的日志文件
func (p *LogParser) ScanNginxLogsForWebsite(websiteID string) []ParserResult {
	if p.demoMode {
		return []ParserResult{}
	}
	if !startIPParsing() {
		return []ParserResult{}
	}
	defer finishIPParsing()

	return p.scanNginxLogsInternal([]string{websiteID})
}

// ResetScanState 重置日志扫描状态
func (p *LogParser) ResetScanState(websiteID string) {
	if websiteID == "" {
		p.states = make(map[string]LogScanState)
	} else {
		delete(p.states, websiteID)
	}
	p.updateState()
}

// TriggerReparse 清空指定网站的日志并触发重新解析
func (p *LogParser) TriggerReparse(websiteID string) error {
	if p.demoMode {
		var err error
		if websiteID == "" {
			err = p.repo.ClearAllLogs()
		} else {
			err = p.repo.ClearLogsForWebsite(websiteID)
		}
		if err != nil {
			return err
		}
		p.ResetScanState(websiteID)
		return nil
	}

	if !startIPParsing() {
		return ErrParsingInProgress
	}

	var ids []string
	if websiteID == "" {
		ids = config.GetAllWebsiteIDs()
	} else {
		ids = []string{websiteID}
	}

	var err error
	if websiteID == "" {
		err = p.repo.ClearAllLogs()
	} else {
		err = p.repo.ClearLogsForWebsite(websiteID)
	}
	if err != nil {
		finishIPParsing()
		return err
	}

	p.ResetScanState(websiteID)

	go func() {
		defer finishIPParsing()
		p.scanNginxLogsInternal(ids)
	}()

	return nil
}

func (p *LogParser) scanNginxLogsInternal(websiteIDs []string) []ParserResult {
	setParsingTotalBytes(p.calculateTotalBytesToScan(websiteIDs))
	parserResults := make([]ParserResult, len(websiteIDs))

	for i, id := range websiteIDs {
		startTime := time.Now()

		website, _ := config.GetWebsiteByID(id)
		parserResult := EmptyParserResult(website.Name, id)

		logPath := website.LogPath
		if strings.Contains(logPath, "*") {
			matches, err := filepath.Glob(logPath)
			if err != nil {
				errstr := "解析日志路径模式 " + logPath + " 失败: " + err.Error()
				parserResult.Success = false
				parserResult.Error = errors.New(errstr)
			} else if len(matches) == 0 {
				errstr := "日志路径模式 " + logPath + " 未匹配到任何文件"
				parserResult.Success = false
				parserResult.Error = errors.New(errstr)
			} else {
				for _, matchPath := range matches {
					p.scanSingleFile(id, matchPath, &parserResult)
				}
			}
		} else {
			p.scanSingleFile(id, logPath, &parserResult)
		}

		parserResult.Duration = time.Since(startTime)
		parserResults[i] = parserResult
	}

	p.updateState()

	return parserResults
}

func (p *LogParser) calculateTotalBytesToScan(websiteIDs []string) int64 {
	var total int64

	for _, id := range websiteIDs {
		website, ok := config.GetWebsiteByID(id)
		if !ok {
			continue
		}

		logPath := website.LogPath
		if strings.Contains(logPath, "*") {
			matches, err := filepath.Glob(logPath)
			if err != nil {
				logrus.Warnf("解析日志路径模式 %s 失败: %v", logPath, err)
				continue
			}
			for _, matchPath := range matches {
				total += p.scanableBytes(id, matchPath)
			}
			continue
		}

		total += p.scanableBytes(id, logPath)
	}

	return total
}

func (p *LogParser) scanableBytes(websiteID, logPath string) int64 {
	fileInfo, err := os.Stat(logPath)
	if err != nil {
		return 0
	}

	currentSize := fileInfo.Size()
	startOffset := p.determineStartOffset(websiteID, logPath, currentSize)
	if isGzipFile(logPath) {
		if startOffset < 0 {
			return 0
		}
		return currentSize
	}
	if currentSize <= startOffset {
		return 0
	}
	return currentSize - startOffset
}

func startIPParsing() bool {
	ipParsingMu.Lock()
	defer ipParsingMu.Unlock()
	if ipParsing {
		return false
	}
	ipParsing = true
	resetParsingProgress()
	return true
}

func finishIPParsing() {
	ipParsingMu.Lock()
	ipParsing = false
	ipParsingMu.Unlock()
	finalizeParsingProgress()
}

func IsIPParsing() bool {
	ipParsingMu.RLock()
	defer ipParsingMu.RUnlock()
	return ipParsing
}

// scanSingleFile 扫描单个日志文件
func (p *LogParser) scanSingleFile(
	websiteID string, logPath string, parserResult *ParserResult) {
	// 打开文件
	file, err := os.Open(logPath)
	if err != nil {
		logrus.Errorf("无法打开日志文件 %s: %v", logPath, err)
		return
	}
	defer file.Close()

	// 获取文件信息
	fileInfo, err := file.Stat()
	if err != nil {
		logrus.Errorf("无法获取文件信息 %s: %v", logPath, err)
		return
	}

	// 确定扫描起始位置
	currentSize := fileInfo.Size()
	startOffset := p.determineStartOffset(websiteID, logPath, currentSize)
	isGzip := isGzipFile(logPath)

	if startOffset < 0 {
		return
	}

	if !isGzip && currentSize <= startOffset {
		return
	}

	var (
		reader io.Reader
		closer io.Closer
	)
	if isGzip {
		if _, err = file.Seek(0, 0); err != nil {
			logrus.Errorf("无法设置文件读取位置 %s: %v", logPath, err)
			return
		}
		gzReader, err := gzip.NewReader(file)
		if err != nil {
			logrus.Errorf("无法解析 gzip 日志文件 %s: %v", logPath, err)
			return
		}
		if startOffset > 0 {
			if err := skipReaderBytes(gzReader, startOffset); err != nil {
				logrus.Warnf("跳过 gzip 历史内容失败，将重新解析文件 %s: %v", logPath, err)
				gzReader.Close()
				if _, err := file.Seek(0, 0); err != nil {
					logrus.Errorf("无法重置 gzip 文件 %s: %v", logPath, err)
					return
				}
				gzReader, err = gzip.NewReader(file)
				if err != nil {
					logrus.Errorf("无法重新解析 gzip 日志文件 %s: %v", logPath, err)
					return
				}
				startOffset = 0
			}
		}
		reader = gzReader
		closer = gzReader
	} else {
		// 设置读取位置
		_, err = file.Seek(startOffset, 0)
		if err != nil {
			logrus.Errorf("无法设置文件读取位置 %s: %v", logPath, err)
			return
		}
		reader = file
	}

	// 读取并解析日志
	entriesCount, bytesRead := p.parseLogLines(reader, websiteID, parserResult)
	if closer != nil {
		closer.Close()
	}

	// 更新文件状态
	if isGzip {
		p.updateFileState(websiteID, logPath, currentSize, startOffset+bytesRead)
	} else {
		p.updateFileState(websiteID, logPath, currentSize, currentSize)
	}

	if entriesCount > 0 {
		logrus.Infof("网站 %s 的日志文件 %s 扫描完成，解析了 %d 条记录",
			websiteID, logPath, entriesCount)
	}
}

// updateFileState 更新文件状态
func (p *LogParser) updateFileState(
	websiteID string, filePath string, currentSize, lastOffset int64) {
	state, ok := p.states[websiteID]
	if !ok {
		state = LogScanState{
			Files: make(map[string]FileState),
		}
	}

	if state.Files == nil {
		state.Files = make(map[string]FileState)
	}

	fileState := FileState{
		LastOffset: lastOffset,
		LastSize:   currentSize,
	}

	state.Files[filePath] = fileState
	p.states[websiteID] = state
}

// determineStartOffset 确定扫描起始位置
func (p *LogParser) determineStartOffset(
	websiteID string, filePath string, currentSize int64) int64 {

	state, ok := p.states[websiteID]
	if !ok { // 网站没有扫描记录，创建新状态
		p.states[websiteID] = LogScanState{
			Files: make(map[string]FileState),
		}
		return 0
	}

	if state.Files == nil {
		state.Files = make(map[string]FileState)
		p.states[websiteID] = state
		return 0
	}

	fileState, ok := state.Files[filePath]
	if !ok {
		return 0
	}

	// 文件是否被轮转
	if currentSize < fileState.LastSize {
		logrus.Infof("检测到网站 %s 的日志文件 %s 已被轮转，从头开始扫描", websiteID, filePath)
		return 0
	}

	if isGzipFile(filePath) {
		if currentSize == fileState.LastSize {
			return -1
		}
		return fileState.LastOffset
	}

	return fileState.LastOffset
}

// parseLogLines 解析日志行并返回解析的记录数
func (p *LogParser) parseLogLines(
	reader io.Reader, websiteID string, parserResult *ParserResult) (int, int64) {
	scanner := bufio.NewScanner(reader)
	entriesCount := 0

	// 批量插入相关
	const batchSize = 100
	batch := make([]store.NginxLogRecord, 0, batchSize)

	// 处理一批数据
	processBatch := func() {
		if len(batch) == 0 {
			return
		}

		p.fillBatchLocations(batch)

		if err := p.repo.BatchInsertLogsForWebsite(websiteID, batch); err != nil {
			logrus.Errorf("批量插入网站 %s 的日志记录失败: %v", websiteID, err)
		}

		batch = batch[:0] // 清空批次但保留容量
	}

	// 逐行处理
	const progressChunk = int64(64 * 1024)
	var pendingBytes int64
	var totalBytes int64
	for scanner.Scan() {
		line := scanner.Text()
		lineBytes := int64(len(line) + 1)
		pendingBytes += lineBytes
		totalBytes += lineBytes
		if pendingBytes >= progressChunk {
			addParsingProgress(pendingBytes)
			pendingBytes = 0
		}

		entry, err := p.parseNginxLogLine(line)
		if err != nil {
			continue
		}
		batch = append(batch, *entry)
		entriesCount++
		parserResult.TotalEntries++ // 累加到总结果中，而非赋值

		if len(batch) >= batchSize {
			processBatch()
		}
	}

	processBatch() // 处理剩余的记录
	if pendingBytes > 0 {
		addParsingProgress(pendingBytes)
	}

	if err := scanner.Err(); err != nil {
		logrus.Errorf("扫描网站 %s 的文件时出错: %v", websiteID, err)
	}

	return entriesCount, totalBytes // 返回当前文件的日志条数
}

func (p *LogParser) fillBatchLocations(batch []store.NginxLogRecord) {
	ips := make([]string, 0, len(batch))
	for _, entry := range batch {
		ips = append(ips, entry.IP)
	}

	locations := enrich.GetIPLocationBatch(ips)
	for i := range batch {
		if location, ok := locations[batch[i].IP]; ok {
			batch[i].DomesticLocation = location.Domestic
			batch[i].GlobalLocation = location.Global
		} else {
			batch[i].DomesticLocation = "未知"
			batch[i].GlobalLocation = "未知"
		}
	}
}

func isGzipFile(filePath string) bool {
	return strings.HasSuffix(strings.ToLower(filePath), ".gz")
}

func skipReaderBytes(reader io.Reader, offset int64) error {
	if offset <= 0 {
		return nil
	}
	_, err := io.CopyN(io.Discard, reader, offset)
	return err
}

// parseNginxLogLine 解析单行Nginx日志
func (p *LogParser) parseNginxLogLine(line string) (*store.NginxLogRecord, error) {
	matches := nginxLogPattern.FindStringSubmatch(line)

	if len(matches) < 10 {
		return nil, errors.New("日志格式不匹配")
	}

	timestamp, err := time.Parse("02/Jan/2006:15:04:05 -0700", matches[3])
	if err != nil {
		return nil, err
	}

	cutoffTime := time.Now().AddDate(0, 0, -p.retentionDays)
	if timestamp.Before(cutoffTime) {
		return nil, errors.New("日志超过保留天数")
	}

	decodedPath, err := url.QueryUnescape(matches[5])
	if err != nil {
		decodedPath = matches[5]
	}
	statusCode, _ := strconv.Atoi(matches[6])
	bytesSent, _ := strconv.Atoi(matches[7])
	referPath, err := url.QueryUnescape(matches[8])
	if err != nil {
		referPath = matches[8]
	}

	pageviewFlag := enrich.ShouldCountAsPageView(statusCode, decodedPath, matches[1])
	browser, os, device := enrich.ParseUserAgent(matches[9])

	return &store.NginxLogRecord{
		ID:               0,
		IP:               matches[1],
		PageviewFlag:     pageviewFlag,
		Timestamp:        timestamp,
		Method:           matches[4],
		Url:              decodedPath,
		Status:           statusCode,
		BytesSent:        bytesSent,
		Referer:          referPath,
		UserBrowser:      browser,
		UserOs:           os,
		UserDevice:       device,
		DomesticLocation: "",
		GlobalLocation:   "",
	}, nil
}

// EmptyParserResult 生成空结果
func EmptyParserResult(name, id string) ParserResult {
	return ParserResult{
		WebName:      name,
		WebID:        id,
		TotalEntries: 0,
		Duration:     0,
		Success:      true,
		Error:        nil,
	}
}
