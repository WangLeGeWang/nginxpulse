package ingest

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
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
	defaultNginxLogRegex = `^(?P<ip>\S+) - (?P<user>\S+) \[(?P<time>[^\]]+)\] "(?P<method>\S+) (?P<url>[^"]+) HTTP/\d\.\d" (?P<status>\d+) (?P<bytes>\d+) "(?P<referer>[^"]*)" "(?P<ua>[^"]*)"`
	lastCleanupDate = ""
	ipParsingMu     sync.RWMutex
	ipParsing       bool
)

const defaultNginxTimeLayout = "02/Jan/2006:15:04:05 -0700"

const (
	parseTypeRegex     = "regex"
	parseTypeCaddyJSON = "caddy_json"
)

var (
	ipAliases        = []string{"ip", "remote_addr", "client_ip"}
	timeAliases      = []string{"time", "time_local", "time_iso8601"}
	methodAliases    = []string{"method", "request_method"}
	urlAliases       = []string{"url", "request_uri", "uri", "path"}
	statusAliases    = []string{"status"}
	bytesAliases     = []string{"bytes", "body_bytes_sent", "bytes_sent"}
	refererAliases   = []string{"referer", "http_referer"}
	userAgentAliases = []string{"ua", "user_agent", "http_user_agent"}
	requestAliases   = []string{"request", "request_line"}
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

type logLineParser struct {
	regex      *regexp.Regexp
	indexMap   map[string]int
	timeLayout string
	source     string
	parseType  string
}

type LogParser struct {
	repo          *store.Repository
	statePath     string
	states        map[string]LogScanState // 各网站的扫描状态，以网站ID为键
	demoMode      bool
	retentionDays int
	lineParsers   map[string]*logLineParser
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
		lineParsers:   make(map[string]*logLineParser),
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
		if _, err := p.getLineParser(id); err != nil {
			parserResult.Success = false
			parserResult.Error = err
			parserResults[i] = parserResult
			continue
		}

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

		entry, err := p.parseLogLine(websiteID, line)
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

func (p *LogParser) getLineParser(websiteID string) (*logLineParser, error) {
	if parser, ok := p.lineParsers[websiteID]; ok {
		return parser, nil
	}

	website, ok := config.GetWebsiteByID(websiteID)
	if !ok {
		return nil, fmt.Errorf("未找到网站配置: %s", websiteID)
	}

	parser, err := newLogLineParser(website)
	if err != nil {
		return nil, err
	}

	p.lineParsers[websiteID] = parser
	return parser, nil
}

func newLogLineParser(website config.WebsiteConfig) (*logLineParser, error) {
	logType := strings.ToLower(strings.TrimSpace(website.LogType))
	if logType == "" {
		logType = "nginx"
	}

	pattern := defaultNginxLogRegex
	source := "default"
	parseType := parseTypeRegex

	if strings.TrimSpace(website.LogRegex) != "" {
		pattern = ensureAnchors(website.LogRegex)
		source = "logRegex"
	} else if strings.TrimSpace(website.LogFormat) != "" {
		compiled, err := buildRegexFromFormat(website.LogFormat)
		if err != nil {
			return nil, err
		}
		pattern = compiled
		source = "logFormat"
	} else if logType == "caddy" {
		return &logLineParser{
			timeLayout: website.TimeLayout,
			source:     "caddy",
			parseType:  parseTypeCaddyJSON,
		}, nil
	} else if logType != "nginx" {
		return nil, fmt.Errorf("不支持的日志类型: %s", logType)
	}

	regex, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("日志格式正则无效 (%s): %w", source, err)
	}

	indexMap := make(map[string]int)
	for i, name := range regex.SubexpNames() {
		if name != "" {
			indexMap[name] = i
		}
	}

	if err := validateLogPattern(indexMap); err != nil {
		return nil, err
	}

	return &logLineParser{
		regex:      regex,
		indexMap:   indexMap,
		timeLayout: website.TimeLayout,
		source:     source,
		parseType:  parseType,
	}, nil
}

func ensureAnchors(pattern string) string {
	trimmed := strings.TrimSpace(pattern)
	if trimmed == "" {
		return trimmed
	}
	if !strings.HasPrefix(trimmed, "^") {
		trimmed = "^" + trimmed
	}
	if !strings.HasSuffix(trimmed, "$") {
		trimmed = trimmed + "$"
	}
	return trimmed
}

func buildRegexFromFormat(format string) (string, error) {
	if strings.TrimSpace(format) == "" {
		return "", errors.New("logFormat 不能为空")
	}

	varPattern := regexp.MustCompile(`\$\w+`)
	locations := varPattern.FindAllStringIndex(format, -1)
	if len(locations) == 0 {
		return "", errors.New("logFormat 未包含任何变量")
	}

	var builder strings.Builder
	usedNames := make(map[string]bool)
	last := 0
	for _, loc := range locations {
		literal := format[last:loc[0]]
		builder.WriteString(regexp.QuoteMeta(literal))

		varName := format[loc[0]+1 : loc[1]]
		builder.WriteString(tokenRegexForVar(varName, usedNames))
		last = loc[1]
	}
	builder.WriteString(regexp.QuoteMeta(format[last:]))

	return "^" + builder.String() + "$", nil
}

func tokenRegexForVar(name string, used map[string]bool) string {
	addGroup := func(group, pattern string) string {
		if used[group] {
			return pattern
		}
		used[group] = true
		return "(?P<" + group + ">" + pattern + ")"
	}

	switch name {
	case "remote_addr":
		return addGroup("ip", `\S+`)
	case "remote_user":
		return addGroup("user", `\S+`)
	case "time_local":
		return addGroup("time", `[^]]+`)
	case "time_iso8601":
		return addGroup("time", `\S+`)
	case "request":
		return addGroup("request", `[^"]+`)
	case "request_method":
		return addGroup("method", `\S+`)
	case "request_uri", "uri":
		return addGroup("url", `\S+`)
	case "status":
		return addGroup("status", `\d{3}`)
	case "body_bytes_sent", "bytes_sent":
		return addGroup("bytes", `\d+`)
	case "http_referer":
		return addGroup("referer", `[^"]*`)
	case "http_user_agent":
		return addGroup("ua", `[^"]*`)
	default:
		return `\S+`
	}
}

func validateLogPattern(indexMap map[string]int) error {
	if len(indexMap) == 0 {
		return errors.New("logRegex/logFormat 必须包含命名分组")
	}

	if !hasAnyField(indexMap, ipAliases) {
		return errors.New("日志格式缺少 IP 字段（ip/remote_addr）")
	}
	if !hasAnyField(indexMap, timeAliases) {
		return errors.New("日志格式缺少时间字段（time/time_local/time_iso8601）")
	}
	if !hasAnyField(indexMap, statusAliases) {
		return errors.New("日志格式缺少状态码字段（status）")
	}
	if !hasAnyField(indexMap, urlAliases) && !hasAnyField(indexMap, requestAliases) {
		return errors.New("日志格式缺少 URL 字段（url/request_uri 或 request）")
	}
	return nil
}

func hasAnyField(indexMap map[string]int, aliases []string) bool {
	for _, name := range aliases {
		if _, ok := indexMap[name]; ok {
			return true
		}
	}
	return false
}

// parseLogLine 解析单行日志
func (p *LogParser) parseLogLine(websiteID string, line string) (*store.NginxLogRecord, error) {
	parser, err := p.getLineParser(websiteID)
	if err != nil {
		return nil, err
	}

	switch parser.parseType {
	case parseTypeCaddyJSON:
		return p.parseCaddyJSONLine(line, parser)
	default:
		return p.parseRegexLogLine(parser, line)
	}
}

func (p *LogParser) parseRegexLogLine(parser *logLineParser, line string) (*store.NginxLogRecord, error) {
	matches := parser.regex.FindStringSubmatch(line)
	if len(matches) == 0 {
		return nil, errors.New("日志格式不匹配")
	}

	ip := extractField(matches, parser.indexMap, ipAliases)
	rawTime := extractField(matches, parser.indexMap, timeAliases)
	statusStr := extractField(matches, parser.indexMap, statusAliases)
	urlValue := extractField(matches, parser.indexMap, urlAliases)
	method := extractField(matches, parser.indexMap, methodAliases)
	requestLine := extractField(matches, parser.indexMap, requestAliases)

	if method == "" || urlValue == "" {
		if requestLine != "" {
			parsedMethod, parsedURL, err := parseRequestLine(requestLine)
			if err != nil {
				return nil, err
			}
			if method == "" {
				method = parsedMethod
			}
			if urlValue == "" {
				urlValue = parsedURL
			}
		}
	}

	if ip == "" || rawTime == "" || statusStr == "" || urlValue == "" {
		return nil, errors.New("日志缺少必要字段")
	}

	timestamp, err := parseLogTime(rawTime, parser.timeLayout)
	if err != nil {
		return nil, err
	}

	statusCode, err := strconv.Atoi(statusStr)
	if err != nil {
		return nil, err
	}

	bytesSent := 0
	bytesStr := extractField(matches, parser.indexMap, bytesAliases)
	if bytesStr != "" && bytesStr != "-" {
		if parsed, err := strconv.Atoi(bytesStr); err == nil {
			bytesSent = parsed
		}
	}

	referPath := extractField(matches, parser.indexMap, refererAliases)

	userAgent := extractField(matches, parser.indexMap, userAgentAliases)
	return p.buildLogRecord(ip, method, urlValue, referPath, userAgent, statusCode, bytesSent, timestamp)
}

func (p *LogParser) parseCaddyJSONLine(line string, parser *logLineParser) (*store.NginxLogRecord, error) {
	decoder := json.NewDecoder(strings.NewReader(line))
	decoder.UseNumber()

	var payload map[string]interface{}
	if err := decoder.Decode(&payload); err != nil {
		return nil, err
	}

	request := getMap(payload, "request")
	headers := getMap(request, "headers")

	ip := getString(request, "remote_ip")
	if ip == "" {
		ip = getString(request, "client_ip")
	}
	if ip == "" {
		ip = getString(payload, "remote_ip")
	}

	method := getString(request, "method")
	urlValue := getString(request, "uri")

	statusCode, ok := getInt(payload, "status")
	if !ok {
		return nil, errors.New("日志缺少状态码")
	}

	bytesSent, _ := getInt(payload, "size")
	referPath := getHeader(headers, "Referer")
	userAgent := getHeader(headers, "User-Agent")

	timestamp, err := parseCaddyTime(payload, parser.timeLayout)
	if err != nil {
		return nil, err
	}

	return p.buildLogRecord(ip, method, urlValue, referPath, userAgent, statusCode, bytesSent, timestamp)
}

func (p *LogParser) buildLogRecord(
	ip, method, urlValue, referer, userAgent string,
	statusCode, bytesSent int, timestamp time.Time) (*store.NginxLogRecord, error) {

	if ip == "" || method == "" || urlValue == "" {
		return nil, errors.New("日志缺少必要字段")
	}
	if statusCode <= 0 {
		return nil, errors.New("日志缺少状态码")
	}

	cutoffTime := time.Now().AddDate(0, 0, -p.retentionDays)
	if timestamp.Before(cutoffTime) {
		return nil, errors.New("日志超过保留天数")
	}

	decodedPath, err := url.QueryUnescape(urlValue)
	if err != nil {
		decodedPath = urlValue
	}

	referPath := referer
	if referPath != "" {
		if decodedRefer, err := url.QueryUnescape(referPath); err == nil {
			referPath = decodedRefer
		}
	}

	if userAgent == "" {
		userAgent = "-"
	}

	pageviewFlag := enrich.ShouldCountAsPageView(statusCode, decodedPath, ip)
	browser, os, device := enrich.ParseUserAgent(userAgent)

	return &store.NginxLogRecord{
		ID:               0,
		IP:               ip,
		PageviewFlag:     pageviewFlag,
		Timestamp:        timestamp,
		Method:           method,
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

func getMap(source map[string]interface{}, key string) map[string]interface{} {
	if source == nil {
		return nil
	}
	value, ok := source[key]
	if !ok {
		return nil
	}
	if mapped, ok := value.(map[string]interface{}); ok {
		return mapped
	}
	return nil
}

func getString(source map[string]interface{}, key string) string {
	if source == nil {
		return ""
	}
	value, ok := source[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	default:
		return fmt.Sprint(typed)
	}
}

func getInt(source map[string]interface{}, key string) (int, bool) {
	if source == nil {
		return 0, false
	}
	value, ok := source[key]
	if !ok || value == nil {
		return 0, false
	}
	switch typed := value.(type) {
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return int(parsed), true
		}
		if parsed, err := typed.Float64(); err == nil {
			return int(parsed), true
		}
	case float64:
		return int(typed), true
	case float32:
		return int(typed), true
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case string:
		if parsed, err := strconv.Atoi(typed); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func getHeader(headers map[string]interface{}, name string) string {
	if headers == nil {
		return ""
	}
	for key, value := range headers {
		if strings.EqualFold(key, name) {
			switch typed := value.(type) {
			case []interface{}:
				if len(typed) > 0 {
					return fmt.Sprint(typed[0])
				}
			case []string:
				if len(typed) > 0 {
					return typed[0]
				}
			case string:
				return typed
			default:
				return fmt.Sprint(typed)
			}
		}
	}
	return ""
}

func parseCaddyTime(payload map[string]interface{}, layout string) (time.Time, error) {
	if payload == nil {
		return time.Time{}, errors.New("日志缺少时间字段")
	}
	if value, ok := payload["ts"]; ok {
		if ts, err := parseAnyTime(value, layout); err == nil {
			return ts, nil
		}
	}
	if value, ok := payload["time"]; ok {
		if ts, err := parseAnyTime(value, layout); err == nil {
			return ts, nil
		}
	}
	if value, ok := payload["timestamp"]; ok {
		if ts, err := parseAnyTime(value, layout); err == nil {
			return ts, nil
		}
	}
	return time.Time{}, errors.New("日志缺少时间字段")
}

func parseAnyTime(value interface{}, layout string) (time.Time, error) {
	switch typed := value.(type) {
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return time.Unix(parsed, 0), nil
		}
		if parsed, err := typed.Float64(); err == nil {
			return parseFloatEpoch(parsed), nil
		}
	case float64:
		return parseFloatEpoch(typed), nil
	case float32:
		return parseFloatEpoch(float64(typed)), nil
	case int:
		return time.Unix(int64(typed), 0), nil
	case int64:
		return time.Unix(typed, 0), nil
	case string:
		return parseLogTime(typed, layout)
	}
	return time.Time{}, errors.New("时间格式不支持")
}

func parseFloatEpoch(value float64) time.Time {
	if value > 1e12 {
		value = value / 1000
	}
	sec := int64(value)
	nsec := int64((value - float64(sec)) * float64(time.Second))
	return time.Unix(sec, nsec)
}

func extractField(matches []string, indexMap map[string]int, aliases []string) string {
	for _, name := range aliases {
		if idx, ok := indexMap[name]; ok {
			if idx > 0 && idx < len(matches) {
				return matches[idx]
			}
		}
	}
	return ""
}

func parseRequestLine(line string) (string, string, error) {
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return "", "", errors.New("无效的 request 格式")
	}
	return parts[0], parts[1], nil
}

func parseLogTime(raw, layout string) (time.Time, error) {
	if ts, ok := parseEpochTime(raw); ok {
		return ts, nil
	}

	layouts := make([]string, 0, 3)
	if layout != "" {
		layouts = append(layouts, layout)
	}
	layouts = append(layouts, defaultNginxTimeLayout, time.RFC3339, time.RFC3339Nano)

	var lastErr error
	for _, l := range layouts {
		parsed, err := time.Parse(l, raw)
		if err == nil {
			return parsed, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("时间解析失败")
	}
	return time.Time{}, lastErr
}

func parseEpochTime(raw string) (time.Time, bool) {
	if raw == "" {
		return time.Time{}, false
	}

	for _, r := range raw {
		if (r < '0' || r > '9') && r != '.' {
			return time.Time{}, false
		}
	}

	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return time.Time{}, false
	}

	if value > 1e12 {
		value = value / 1000
	}

	sec := int64(value)
	nsec := int64((value - float64(sec)) * float64(time.Second))
	return time.Unix(sec, nsec), true
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
