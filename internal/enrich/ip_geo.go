package enrich

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/likaia/nginxpulse/internal/config"
	"github.com/lionsoul2014/ip2region/binding/golang/xdb"
	"github.com/sirupsen/logrus"
)

//go:embed data/ip2region_v4.xdb data/ip2region_v6.xdb
var ipDataFiles embed.FS

var (
	ipSearcherV4  *xdb.Searcher
	ipSearcherV6  *xdb.Searcher
	vectorIndexV4 []byte
	vectorIndexV6 []byte
	dbPathV4      = filepath.Join(config.DataDir, "ip2region_v4.xdb")
	dbPathV6      = filepath.Join(config.DataDir, "ip2region_v6.xdb")
)

const (
	ipAPIFields    = "status,message,country,countryCode,region,regionName,city,isp,query"
	ipAPITimeout   = 1200 * time.Millisecond
	maxIPCacheSize = 50000
	ipAPIBatchSize = 100
)

type IPLocation struct {
	Domestic string
	Global   string
	Source   string
}

type ipLocationCacheEntry struct {
	Domestic string
	Global   string
	Updated  time.Time
}

var (
	ipGeoCache   = make(map[string]ipLocationCacheEntry)
	ipGeoCacheMu sync.RWMutex
)

type ipAPIBatchRequest struct {
	Query  string `json:"query"`
	Fields string `json:"fields"`
	Lang   string `json:"lang"`
}

type ipAPIBatchResponse struct {
	Status      string `json:"status"`
	Message     string `json:"message"`
	Country     string `json:"country"`
	CountryCode string `json:"countryCode"`
	Region      string `json:"region"`
	RegionName  string `json:"regionName"`
	City        string `json:"city"`
	ISP         string `json:"isp"`
	Query       string `json:"query"`
}

type ipRegionParts struct {
	Country  string
	Province string
	City     string
	ISP      string
}

// ExtractIPRegionDBs 从嵌入的文件系统中提取 IP2Region 数据库
func ExtractIPRegionDBs() (string, string, error) {
	// 确保数据目录存在
	if _, err := os.Stat(config.DataDir); os.IsNotExist(err) {
		if err := os.MkdirAll(config.DataDir, 0755); err != nil {
			return "", "", err
		}
	}

	// 目标文件路径
	v4Path := filepath.Join(config.DataDir, "ip2region_v4.xdb")
	v6Path := filepath.Join(config.DataDir, "ip2region_v6.xdb")

	if err := extractIPRegionDBFile("data/ip2region_v4.xdb", v4Path, "IP2Region v4"); err != nil {
		return "", "", err
	}

	if err := extractIPRegionDBFile("data/ip2region_v6.xdb", v6Path, "IP2Region v6"); err != nil {
		return "", "", err
	}

	return v4Path, v6Path, nil
}

func extractIPRegionDBFile(embedPath, targetPath, label string) error {
	// 检查文件是否已存在
	if _, err := os.Stat(targetPath); err == nil {
		logrus.Infof("%s 数据库已存在，跳过提取", label)
		return nil
	}

	// 从嵌入文件系统读取数据
	data, err := fs.ReadFile(ipDataFiles, embedPath)
	if err != nil {
		return err
	}

	// 写入文件
	if err := os.WriteFile(targetPath, data, 0644); err != nil {
		return err
	}

	logrus.Infof("%s 数据库已成功提取", label)
	return nil
}

// InitIPGeoLocation 初始化 IP 地理位置查询
func InitIPGeoLocation() error {
	// 从嵌入的文件系统中提取数据库文件
	v4Path, v6Path, err := ExtractIPRegionDBs()
	if err != nil {
		return fmt.Errorf("提取 ip2region 数据库失败: %v", err)
	}

	// 更新数据库路径
	dbPathV4 = v4Path
	dbPathV6 = v6Path

	searcherV4, vIndexV4, err := initIPSearcher(dbPathV4, "v4")
	if err != nil {
		return err
	}

	searcherV6, vIndexV6, err := initIPSearcher(dbPathV6, "v6")
	if err != nil {
		return err
	}

	ipSearcherV4 = searcherV4
	ipSearcherV6 = searcherV6
	vectorIndexV4 = vIndexV4
	vectorIndexV6 = vIndexV6
	logrus.Info("ip2region 初始化成功")
	return nil
}

func initIPSearcher(path, label string) (*xdb.Searcher, []byte, error) {
	header, err := xdb.LoadHeaderFromFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("读取 ip2region %s 数据库头失败: %v", label, err)
	}

	version, err := xdb.VersionFromHeader(header)
	if err != nil {
		return nil, nil, fmt.Errorf("识别 ip2region %s 版本失败: %v", label, err)
	}

	vIndex, err := xdb.LoadVectorIndexFromFile(path)
	if err != nil {
		logrus.Warnf("加载 ip2region %s 矢量索引失败，将使用全量搜索: %v", label, err)
	}

	searcher, err := xdb.NewWithVectorIndex(version, path, vIndex)
	if err != nil {
		return nil, nil, fmt.Errorf("创建 ip2region %s 搜索器失败: %v", label, err)
	}

	return searcher, vIndex, nil
}

// GetIPLocation 获取 IP 的地理位置信息
func GetIPLocation(ip string) (string, string, error) {
	// 处理无效 IP
	if ip == "" || ip == "localhost" || ip == "127.0.0.1" || ip == "::1" {
		return "本地", "本地", nil
	}

	if domestic, global, ok := getCachedLocation(ip); ok {
		return domestic, global, nil
	}

	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return "未知", "未知", fmt.Errorf("无效的 IP 地址")
	}

	// 检查是否是内网 IP
	if isPrivateIP(parsedIP) {
		return "内网", "本地网络", nil
	}

	localDomestic, localGlobal, localHasCity, localErr := queryIPLocationLocalDetailed(ip, parsedIP)
	localUsable := localErr == nil && localDomestic != "" && localDomestic != "未知" && localGlobal != "" && localGlobal != "未知"
	if localUsable && localHasCity {
		setCachedLocation(ip, localDomestic, localGlobal)
		return localDomestic, localGlobal, nil
	}

	remoteDomestic, remoteGlobal, remoteErr := queryIPLocationRemote(ip)
	if remoteErr == nil {
		if remoteDomestic == "" {
			remoteDomestic = "未知"
		}
		if remoteGlobal == "" {
			remoteGlobal = "未知"
		}
		if remoteDomestic != "未知" && remoteGlobal != "未知" {
			setCachedLocation(ip, remoteDomestic, remoteGlobal)
			return remoteDomestic, remoteGlobal, nil
		}
	}

	if localUsable {
		setCachedLocation(ip, localDomestic, localGlobal)
		return localDomestic, localGlobal, nil
	}

	if remoteErr != nil {
		return "未知", "未知", remoteErr
	}
	setCachedLocation(ip, "未知", "未知")
	return "未知", "未知", nil
}

// GetIPLocationBatch 批量获取 IP 的地理位置信息（优先本地）
func GetIPLocationBatch(ips []string) (map[string]IPLocation, error) {
	results := make(map[string]IPLocation, len(ips))
	if len(ips) == 0 {
		return results, nil
	}

	unique := make([]string, 0, len(ips))
	seen := make(map[string]struct{}, len(ips))
	for _, raw := range ips {
		ip := strings.TrimSpace(raw)
		if ip == "" {
			continue
		}
		if _, ok := seen[ip]; ok {
			continue
		}
		seen[ip] = struct{}{}
		unique = append(unique, ip)
	}

	toQuery := make([]string, 0, len(unique))
	fallbacks := make(map[string]IPLocation, len(unique))
	for _, ip := range unique {
		if domestic, global, ok := getCachedLocation(ip); ok {
			results[ip] = IPLocation{Domestic: domestic, Global: global, Source: "cache"}
			continue
		}

		if ip == "localhost" || ip == "127.0.0.1" || ip == "::1" {
			results[ip] = IPLocation{Domestic: "本地", Global: "本地", Source: "local"}
			setCachedLocation(ip, "本地", "本地")
			continue
		}

		parsedIP := net.ParseIP(ip)
		if parsedIP == nil {
			results[ip] = IPLocation{Domestic: "未知", Global: "未知", Source: "invalid"}
			setCachedLocation(ip, "未知", "未知")
			continue
		}

		if isPrivateIP(parsedIP) {
			results[ip] = IPLocation{Domestic: "内网", Global: "本地网络", Source: "local"}
			setCachedLocation(ip, "内网", "本地网络")
			continue
		}

		localDomestic, localGlobal, localHasCity, localErr := queryIPLocationLocalDetailed(ip, parsedIP)
		localUsable := localErr == nil && localDomestic != "" && localDomestic != "未知" && localGlobal != "" && localGlobal != "未知"
		if localUsable && localHasCity {
			results[ip] = IPLocation{Domestic: localDomestic, Global: localGlobal, Source: "local"}
			setCachedLocation(ip, localDomestic, localGlobal)
			continue
		}
		if localUsable {
			fallbacks[ip] = IPLocation{Domestic: localDomestic, Global: localGlobal, Source: "local"}
		}
		toQuery = append(toQuery, ip)
	}

	if len(toQuery) == 0 {
		return results, nil
	}

	remoteResults, remoteErr := queryIPLocationRemoteBatch(toQuery)
	for _, ip := range toQuery {
		if entry, ok := remoteResults[ip]; ok && entry.Domestic != "" && entry.Domestic != "未知" && entry.Global != "" && entry.Global != "未知" {
			results[ip] = IPLocation{Domestic: entry.Domestic, Global: entry.Global, Source: "remote"}
			setCachedLocation(ip, entry.Domestic, entry.Global)
			continue
		}
		if fallback, ok := fallbacks[ip]; ok {
			results[ip] = fallback
			setCachedLocation(ip, fallback.Domestic, fallback.Global)
			continue
		}
		if remoteErr == nil {
			results[ip] = IPLocation{Domestic: "未知", Global: "未知", Source: "unknown"}
			setCachedLocation(ip, "未知", "未知")
		}
	}

	if len(results) > 0 {
		return results, nil
	}
	return results, remoteErr
}

func queryIPLocationLocalRegion(ip string, parsedIP net.IP) (string, error) {
	searcher, err := pickIPSearcher(parsedIP)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	resultCh := make(chan struct {
		region string
		err    error
	}, 1)

	go func() {
		region, err := searcher.SearchByStr(ip)
		resultCh <- struct {
			region string
			err    error
		}{region, err}
	}()

	select {
	case <-ctx.Done():
		return "", fmt.Errorf("IP 查询超时")
	case result := <-resultCh:
		if result.err != nil {
			return "", result.err
		}
		return result.region, nil
	}
}

func pickIPSearcher(ip net.IP) (*xdb.Searcher, error) {
	if ip == nil {
		return nil, fmt.Errorf("无效的 IP 地址")
	}

	if ip.To4() != nil {
		if ipSearcherV4 == nil {
			return nil, fmt.Errorf("ip2region v4 未初始化")
		}
		return ipSearcherV4, nil
	}

	if ip.To16() != nil {
		if ipSearcherV6 == nil {
			return nil, fmt.Errorf("ip2region v6 未初始化")
		}
		return ipSearcherV6, nil
	}

	return nil, fmt.Errorf("无效的 IP 地址")
}

func queryIPLocationLocalDetailed(ip string, parsedIP net.IP) (string, string, bool, error) {
	if parsedIP == nil {
		parsedIP = net.ParseIP(ip)
		if parsedIP == nil {
			return "未知", "未知", false, fmt.Errorf("无效的 IP 地址")
		}
	}

	region, err := queryIPLocationLocalRegion(ip, parsedIP)
	if err != nil {
		return "未知", "未知", false, err
	}
	domestic, global, err := parseIPRegion(region)
	if err != nil {
		return domestic, global, false, err
	}
	parts := parseIPRegionParts(region)
	city := removeSuffixes(strings.TrimSpace(parts.City))
	if isISPLabel(city) {
		city = ""
	}
	hasCity := city != "" && city != "0" && city != "未知"
	return domestic, global, hasCity, nil
}

// 查询 IP 地理位置（本地库）
func queryIPLocationLocal(ip string) (string, string, error) {
	domestic, global, _, err := queryIPLocationLocalDetailed(ip, nil)
	return domestic, global, err
}

// 查询 IP 地理位置（远程接口）
func queryIPLocationRemote(ip string) (string, string, error) {
	results, err := queryIPLocationRemoteBatch([]string{ip})
	if err != nil {
		return "未知", "未知", err
	}
	entry, ok := results[ip]
	if !ok {
		return "未知", "未知", fmt.Errorf("ip-api 返回为空")
	}
	return entry.Domestic, entry.Global, nil
}

func queryIPLocationRemoteBatch(ips []string) (map[string]ipLocationCacheEntry, error) {
	results := make(map[string]ipLocationCacheEntry, len(ips))
	if len(ips) == 0 {
		return results, nil
	}

	client := &http.Client{Timeout: ipAPITimeout}
	var lastErr error
	apiURL := resolveIPAPIURL()

	for start := 0; start < len(ips); start += ipAPIBatchSize {
		end := start + ipAPIBatchSize
		if end > len(ips) {
			end = len(ips)
		}

		batch := ips[start:end]
		language := resolveIPAPILanguage()
		requestPayload := make([]ipAPIBatchRequest, 0, len(batch))
		for _, ip := range batch {
			requestPayload = append(requestPayload, ipAPIBatchRequest{
				Query:  ip,
				Fields: ipAPIFields,
				Lang:   language,
			})
		}

		requestBody, err := json.Marshal(requestPayload)
		if err != nil {
			lastErr = err
			continue
		}

		req, err := http.NewRequest(http.MethodPost, apiURL, bytes.NewReader(requestBody))
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "nginxpulse/1.0")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("ip-api 响应异常: %s", resp.Status)
			resp.Body.Close()
			continue
		}

		var payload []ipAPIBatchResponse
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			lastErr = err
			resp.Body.Close()
			continue
		}
		resp.Body.Close()

		for i, item := range payload {
			query := strings.TrimSpace(item.Query)
			if query == "" && i < len(batch) {
				query = batch[i]
			}
			if query == "" {
				continue
			}

			if item.Status != "" && item.Status != "success" {
				results[query] = ipLocationCacheEntry{Domestic: "未知", Global: "未知"}
				continue
			}

			domestic := formatDomesticLocation(item.Country, item.CountryCode, item.RegionName, item.City)
			global := formatGlobalLocation(item.Country)
			if domestic == "" {
				domestic = "未知"
			}
			if global == "" {
				global = "未知"
			}
			results[query] = ipLocationCacheEntry{Domestic: domestic, Global: global}
		}
	}

	return results, lastErr
}

// 解析 ip2region 返回的地区信息
func parseIPRegion(region string) (string, string, error) {
	// 返回格式: 国家|区域|省份|城市|ISP
	parts := parseIPRegionParts(region)
	var domestic, global string

	// 国内
	if parts.Country == "中国" {
		province := removeSuffixes(parts.Province)
		city := removeSuffixes(parts.City)
		if isISPLabel(city) {
			city = ""
		}
		if province != "" && province != "0" {
			if city != "" && city != "0" && city != province {
				domestic = fmt.Sprintf("%s·%s", province, city)
			} else {
				domestic = province
			}
		} else if city != "" && city != "0" {
			domestic = city
		} else {
			domestic = "中国"
		}
	} else if parts.Country == "0" || parts.Country == "" {
		domestic = "未知"
	} else {
		domestic = joinLocationParts(parts.Country, parts.Province, parts.City)
	}

	// 全球
	if parts.Country != "0" && parts.Country != "" {
		global = parts.Country
	} else {
		global = "未知"
	}

	return domestic, global, nil
}

// 解析 ip2region
func splitRegion(region string) []string {
	parts := make([]string, 5)
	fields := bytes.Split([]byte(region), []byte("|"))

	for i := 0; i < len(fields) && i < 5; i++ {
		parts[i] = string(fields[i])
	}

	return parts
}

func parseIPRegionParts(region string) ipRegionParts {
	parts := splitRegion(region)
	country := strings.TrimSpace(parts[0])
	p1 := strings.TrimSpace(parts[1])
	p2 := strings.TrimSpace(parts[2])
	p3 := strings.TrimSpace(parts[3])
	p4 := strings.TrimSpace(parts[4])

	result := ipRegionParts{Country: country}

	switch {
	case isISPLabel(p4):
		result.Province = p2
		result.City = p3
		result.ISP = p4
	case isISPLabel(p3) && !isISPLabel(p2):
		result.Province = p1
		result.City = p2
		result.ISP = p3
	default:
		result.Province = p2
		result.City = p3
	}

	return result
}

func formatDomesticLocation(country, countryCode, regionName, city string) string {
	country = strings.TrimSpace(country)
	countryCode = strings.TrimSpace(countryCode)
	if country == "" || country == "0" {
		return "未知"
	}
	if country != "中国" && !strings.EqualFold(country, "china") && !strings.EqualFold(countryCode, "CN") {
		return joinLocationParts(country, regionName, city)
	}
	province := removeSuffixes(strings.TrimSpace(regionName))
	city = removeSuffixes(strings.TrimSpace(city))
	if province == "" && city == "" {
		if country != "" && country != "0" {
			return country
		}
		return "中国"
	}
	if province != "" && city != "" && province == city {
		return province
	}
	return joinLocationParts(province, city)
}

func formatGlobalLocation(country string) string {
	country = strings.TrimSpace(country)
	if country == "" || country == "0" {
		return "未知"
	}
	return country
}

func joinLocationParts(parts ...string) string {
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		clean := normalizeLocationPart(part)
		if clean != "" {
			normalized = append(normalized, clean)
		}
	}
	if len(normalized) == 0 {
		return "未知"
	}
	return strings.Join(normalized, "·")
}

func normalizeLocationPart(value string) string {
	clean := strings.TrimSpace(value)
	if clean == "" || clean == "0" || clean == "未知" {
		return ""
	}
	return clean
}

func resolveIPAPILanguage() string {
	switch config.GetLanguage() {
	case config.EnglishLanguage:
		return "en"
	default:
		return config.DefaultLanguage
	}
}

func resolveIPAPIURL() string {
	return config.GetIPGeoAPIURL()
}

func getCachedLocation(ip string) (string, string, bool) {
	ipGeoCacheMu.RLock()
	entry, ok := ipGeoCache[ip]
	ipGeoCacheMu.RUnlock()
	if !ok {
		return "", "", false
	}
	return entry.Domestic, entry.Global, true
}

func setCachedLocation(ip, domestic, global string) {
	if ip == "" {
		return
	}
	ipGeoCacheMu.Lock()
	defer ipGeoCacheMu.Unlock()
	if len(ipGeoCache) >= maxIPCacheSize {
		ipGeoCache = make(map[string]ipLocationCacheEntry)
	}
	ipGeoCache[ip] = ipLocationCacheEntry{
		Domestic: domestic,
		Global:   global,
		Updated:  time.Now(),
	}
}

// ResetIPGeoCache clears in-memory IP geo cache entries.
func ResetIPGeoCache() {
	ipGeoCacheMu.Lock()
	defer ipGeoCacheMu.Unlock()
	ipGeoCache = make(map[string]ipLocationCacheEntry)
}

// 是否是内网 IP
func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}

	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}

	if v4 := ip.To4(); v4 != nil {
		switch {
		case v4[0] == 10:
			return true
		case v4[0] == 172 && v4[1] >= 16 && v4[1] <= 31:
			return true
		case v4[0] == 192 && v4[1] == 168:
			return true
		case v4[0] == 127:
			return true
		case v4[0] == 169 && v4[1] == 254:
			return true
		default:
			return false
		}
	}

	ip = ip.To16()
	if ip == nil {
		return false
	}

	// IPv6 ULA fc00::/7
	if ip[0]&0xfe == 0xfc {
		return true
	}

	return false
}

// 去掉地区名称后缀
func removeSuffixes(name string) string {
	suffixes := []string{"省", "市", "自治区", "维吾尔自治区", "壮族自治区", "回族自治区", "特别行政区"}
	for _, suffix := range suffixes {
		if len(name) > len(suffix) && name[len(name)-len(suffix):] == suffix {
			return name[:len(name)-len(suffix)]
		}
	}
	return name
}

func isISPLabel(value string) bool {
	clean := strings.TrimSpace(value)
	if clean == "" || clean == "0" || clean == "未知" {
		return false
	}
	regionSuffixes := []string{"省", "市", "自治区", "地区", "盟", "州", "县", "区", "特别行政区"}
	for _, suffix := range regionSuffixes {
		if strings.HasSuffix(clean, suffix) {
			return false
		}
	}
	ispKeywords := []string{
		"电信", "联通", "移动", "铁通", "广电", "网通", "教育网", "长城宽带", "有线", "鹏博士", "阿里",
	}
	for _, keyword := range ispKeywords {
		if strings.Contains(clean, keyword) {
			return true
		}
	}
	return false
}
