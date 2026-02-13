package web

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/likaia/nginxpulse/internal/analytics"
	"github.com/likaia/nginxpulse/internal/config"
	"github.com/xuri/excelize/v2"
)

const exportBatchSize = 1000
const logsExportContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

const (
	logsExportSheetName = "Logs"
	logsHeaderRow       = 3
	logsDataStartRow    = 4
	logsLastColumn      = "L"
)

var ErrExportCanceled = fmt.Errorf("export canceled")

type exportProgressFunc func(processed, total int64)
type exportCancelFunc func() bool

type logsExportStyles struct {
	siteLabel   int
	siteValue   int
	header      int
	data        int
	dataAlt     int
	statusError int
	pvYes       int
}

func exportLogsXLSX(
	writer io.Writer,
	statsFactory *analytics.StatsFactory,
	query analytics.StatsQuery,
	lang string,
) error {
	return exportLogsXLSXWithProgress(writer, statsFactory, query, lang, nil, nil)
}

func exportLogsXLSXWithProgress(
	writer io.Writer,
	statsFactory *analytics.StatsFactory,
	query analytics.StatsQuery,
	lang string,
	onProgress exportProgressFunc,
	shouldCancel exportCancelFunc,
) error {
	manager, ok := statsFactory.GetManager("logs")
	if !ok {
		return fmt.Errorf("日志管理器未初始化")
	}

	normalizedLang := normalizeExportLang(lang)

	file := excelize.NewFile()
	defer file.Close()

	defaultSheet := file.GetSheetName(0)
	if defaultSheet != logsExportSheetName {
		file.SetSheetName(defaultSheet, logsExportSheetName)
	}

	styles, err := createLogsExportStyles(file)
	if err != nil {
		return err
	}

	if err := setupLogsExportSheet(file, query, normalizedLang, styles); err != nil {
		return err
	}

	currentRow := logsDataStartRow
	dataRowIndex := 0
	var processed int64
	var total int64

	for page := 1; ; page++ {
		if shouldCancel != nil && shouldCancel() {
			return ErrExportCanceled
		}

		query.ExtraParam["page"] = page
		query.ExtraParam["pageSize"] = exportBatchSize

		result, err := manager.Query(query)
		if err != nil {
			return err
		}
		logsResult, ok := result.(analytics.LogsStats)
		if !ok {
			return fmt.Errorf("日志导出结果解析失败")
		}
		if len(logsResult.Logs) == 0 {
			break
		}

		for i, log := range logsResult.Logs {
			if shouldCancel != nil && i%200 == 0 && shouldCancel() {
				return ErrExportCanceled
			}

			rowCell, err := excelize.CoordinatesToCellName(1, currentRow)
			if err != nil {
				return err
			}
			rowValues := buildLogExportRowValues(log, normalizedLang)
			if err := file.SetSheetRow(logsExportSheetName, rowCell, &rowValues); err != nil {
				return err
			}

			startCell := fmt.Sprintf("A%d", currentRow)
			endCell := fmt.Sprintf("%s%d", logsLastColumn, currentRow)
			rowStyle := styles.data
			if dataRowIndex%2 == 1 {
				rowStyle = styles.dataAlt
			}
			if err := file.SetCellStyle(logsExportSheetName, startCell, endCell, rowStyle); err != nil {
				return err
			}

			if log.StatusCode >= 400 {
				statusCell := fmt.Sprintf("E%d", currentRow)
				if err := file.SetCellStyle(logsExportSheetName, statusCell, statusCell, styles.statusError); err != nil {
					return err
				}
			}
			if log.PageviewFlag {
				pvCell := fmt.Sprintf("L%d", currentRow)
				if err := file.SetCellStyle(logsExportSheetName, pvCell, pvCell, styles.pvYes); err != nil {
					return err
				}
			}

			currentRow++
			dataRowIndex++
		}

		processed += int64(len(logsResult.Logs))
		if total == 0 && logsResult.Pagination.Total > 0 {
			total = int64(logsResult.Pagination.Total)
		}
		if onProgress != nil {
			onProgress(processed, total)
		}

		if logsResult.Pagination.Pages > 0 && page >= logsResult.Pagination.Pages {
			break
		}
	}

	return file.Write(writer)
}

func setupLogsExportSheet(file *excelize.File, query analytics.StatsQuery, lang string, styles logsExportStyles) error {
	websiteRow := logsExportWebsiteRow(query, lang)
	topRow := []interface{}{websiteRow[0], websiteRow[1]}
	if err := file.SetSheetRow(logsExportSheetName, "A1", &topRow); err != nil {
		return err
	}
	if err := file.MergeCell(logsExportSheetName, "B1", fmt.Sprintf("%s1", logsLastColumn)); err != nil {
		return err
	}
	if err := file.SetCellStyle(logsExportSheetName, "A1", "A1", styles.siteLabel); err != nil {
		return err
	}
	if err := file.SetCellStyle(logsExportSheetName, "B1", fmt.Sprintf("%s1", logsLastColumn), styles.siteValue); err != nil {
		return err
	}

	headers := logsExportHeaders(lang)
	headerValues := make([]interface{}, 0, len(headers))
	for _, header := range headers {
		headerValues = append(headerValues, header)
	}
	if err := file.SetSheetRow(logsExportSheetName, fmt.Sprintf("A%d", logsHeaderRow), &headerValues); err != nil {
		return err
	}
	if err := file.SetCellStyle(logsExportSheetName, fmt.Sprintf("A%d", logsHeaderRow), fmt.Sprintf("%s%d", logsLastColumn, logsHeaderRow), styles.header); err != nil {
		return err
	}

	if err := file.SetRowHeight(logsExportSheetName, 1, 28); err != nil {
		return err
	}
	if err := file.SetRowHeight(logsExportSheetName, logsHeaderRow, 24); err != nil {
		return err
	}

	if err := file.SetColWidth(logsExportSheetName, "A", "A", 21); err != nil {
		return err
	}
	if err := file.SetColWidth(logsExportSheetName, "B", "B", 16); err != nil {
		return err
	}
	if err := file.SetColWidth(logsExportSheetName, "C", "C", 24); err != nil {
		return err
	}
	if err := file.SetColWidth(logsExportSheetName, "D", "D", 58); err != nil {
		return err
	}
	if err := file.SetColWidth(logsExportSheetName, "E", "E", 10); err != nil {
		return err
	}
	if err := file.SetColWidth(logsExportSheetName, "F", "F", 14); err != nil {
		return err
	}
	if err := file.SetColWidth(logsExportSheetName, "G", "G", 14); err != nil {
		return err
	}
	if err := file.SetColWidth(logsExportSheetName, "H", "H", 30); err != nil {
		return err
	}
	if err := file.SetColWidth(logsExportSheetName, "I", "I", 20); err != nil {
		return err
	}
	if err := file.SetColWidth(logsExportSheetName, "J", "J", 18); err != nil {
		return err
	}
	if err := file.SetColWidth(logsExportSheetName, "K", "K", 14); err != nil {
		return err
	}
	if err := file.SetColWidth(logsExportSheetName, "L", "L", 10); err != nil {
		return err
	}

	if err := file.SetPanes(logsExportSheetName, &excelize.Panes{
		Freeze:      true,
		Split:       false,
		XSplit:      0,
		YSplit:      logsDataStartRow - 1,
		TopLeftCell: fmt.Sprintf("A%d", logsDataStartRow),
		ActivePane:  "bottomLeft",
	}); err != nil {
		return err
	}

	if err := file.AutoFilter(logsExportSheetName, fmt.Sprintf("A%d:%s%d", logsHeaderRow, logsLastColumn, logsHeaderRow), nil); err != nil {
		return err
	}

	return nil
}

func createLogsExportStyles(file *excelize.File) (logsExportStyles, error) {
	siteLabel, err := file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 12, Color: "#B42318"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#FEE4E2"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border: []excelize.Border{
			{Type: "left", Color: "#EAECF0", Style: 1},
			{Type: "right", Color: "#EAECF0", Style: 1},
			{Type: "top", Color: "#EAECF0", Style: 1},
			{Type: "bottom", Color: "#EAECF0", Style: 1},
		},
	})
	if err != nil {
		return logsExportStyles{}, err
	}

	siteValue, err := file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 14, Color: "#B42318"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#FFF1F3"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "left", Vertical: "center"},
		Border: []excelize.Border{
			{Type: "left", Color: "#EAECF0", Style: 1},
			{Type: "right", Color: "#EAECF0", Style: 1},
			{Type: "top", Color: "#EAECF0", Style: 1},
			{Type: "bottom", Color: "#EAECF0", Style: 1},
		},
	})
	if err != nil {
		return logsExportStyles{}, err
	}

	header, err := file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 11, Color: "#FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#1D3557"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border: []excelize.Border{
			{Type: "left", Color: "#1D3557", Style: 1},
			{Type: "right", Color: "#1D3557", Style: 1},
			{Type: "top", Color: "#1D3557", Style: 1},
			{Type: "bottom", Color: "#1D3557", Style: 1},
		},
	})
	if err != nil {
		return logsExportStyles{}, err
	}

	data, err := file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 10, Color: "#1F2937"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#FFFFFF"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "left", Vertical: "center", WrapText: true},
		Border: []excelize.Border{
			{Type: "left", Color: "#E5E7EB", Style: 1},
			{Type: "right", Color: "#E5E7EB", Style: 1},
			{Type: "top", Color: "#E5E7EB", Style: 1},
			{Type: "bottom", Color: "#E5E7EB", Style: 1},
		},
	})
	if err != nil {
		return logsExportStyles{}, err
	}

	dataAlt, err := file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Size: 10, Color: "#1F2937"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#F8FAFC"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "left", Vertical: "center", WrapText: true},
		Border: []excelize.Border{
			{Type: "left", Color: "#E5E7EB", Style: 1},
			{Type: "right", Color: "#E5E7EB", Style: 1},
			{Type: "top", Color: "#E5E7EB", Style: 1},
			{Type: "bottom", Color: "#E5E7EB", Style: 1},
		},
	})
	if err != nil {
		return logsExportStyles{}, err
	}

	statusError, err := file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "#B42318"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#FEE4E2"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border: []excelize.Border{
			{Type: "left", Color: "#E5E7EB", Style: 1},
			{Type: "right", Color: "#E5E7EB", Style: 1},
			{Type: "top", Color: "#E5E7EB", Style: 1},
			{Type: "bottom", Color: "#E5E7EB", Style: 1},
		},
	})
	if err != nil {
		return logsExportStyles{}, err
	}

	pvYes, err := file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "#067647"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"#ECFDF3"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border: []excelize.Border{
			{Type: "left", Color: "#E5E7EB", Style: 1},
			{Type: "right", Color: "#E5E7EB", Style: 1},
			{Type: "top", Color: "#E5E7EB", Style: 1},
			{Type: "bottom", Color: "#E5E7EB", Style: 1},
		},
	})
	if err != nil {
		return logsExportStyles{}, err
	}

	return logsExportStyles{
		siteLabel:   siteLabel,
		siteValue:   siteValue,
		header:      header,
		data:        data,
		dataAlt:     dataAlt,
		statusError: statusError,
		pvYes:       pvYes,
	}, nil
}

func buildLogExportRow(log analytics.LogEntry, lang string) []string {
	location := strings.TrimSpace(log.DomesticLocation)
	if location == "" {
		location = strings.TrimSpace(log.GlobalLocation)
	}
	if location == "" {
		location = "-"
	}

	requestText := strings.TrimSpace(fmt.Sprintf("%s %s", log.Method, log.URL))
	if requestText == "" {
		requestText = "-"
	}

	referer := strings.TrimSpace(log.Referer)
	if referer == "" {
		referer = "-"
	}

	browser := strings.TrimSpace(log.UserBrowser)
	if browser == "" {
		browser = "-"
	}

	os := strings.TrimSpace(log.UserOS)
	if os == "" {
		os = "-"
	}

	device := strings.TrimSpace(log.UserDevice)
	if device == "" {
		device = "-"
	}

	pvText := "否"
	if lang == config.EnglishLanguage {
		pvText = "No"
	}
	if log.PageviewFlag {
		pvText = "是"
		if lang == config.EnglishLanguage {
			pvText = "Yes"
		}
	}

	timeText := log.Time
	if timeText == "" && log.Timestamp > 0 {
		timeText = time.Unix(log.Timestamp, 0).Format("2006-01-02 15:04:05")
	}

	return []string{
		timeText,
		log.IP,
		location,
		requestText,
		strconv.Itoa(log.StatusCode),
		strconv.FormatInt(int64(log.BytesSent), 10),
		formatTraffic(int64(log.BytesSent)),
		referer,
		browser,
		os,
		device,
		pvText,
	}
}

func buildLogExportRowValues(log analytics.LogEntry, lang string) []interface{} {
	row := buildLogExportRow(log, lang)
	values := make([]interface{}, 0, len(row))
	for idx, value := range row {
		if idx == 5 {
			values = append(values, int64(log.BytesSent))
			continue
		}
		values = append(values, value)
	}
	return values
}

func logsExportHeaders(lang string) []string {
	if lang == config.EnglishLanguage {
		return []string{
			"Time",
			"IP",
			"Location",
			"Request",
			"Status",
			"Bytes (Raw)",
			"Traffic",
			"Referer",
			"Browser",
			"OS",
			"Device",
			"PV",
		}
	}
	return []string{
		"时间",
		"IP",
		"位置",
		"请求",
		"状态码",
		"流量(字节)",
		"流量(可读)",
		"来源",
		"浏览器",
		"系统",
		"设备",
		"PV",
	}
}

func formatTraffic(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%.2f B", float64(bytes))
	}
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.2f KB", float64(bytes)/1024)
	}
	if bytes < 1024*1024*1024 {
		return fmt.Sprintf("%.2f MB", float64(bytes)/(1024*1024))
	}
	if bytes < 1024*1024*1024*1024 {
		return fmt.Sprintf("%.2f GB", float64(bytes)/(1024*1024*1024))
	}
	return fmt.Sprintf("%.2f TB", float64(bytes)/(1024*1024*1024*1024))
}

func logsExportWebsiteRow(query analytics.StatsQuery, lang string) []string {
	label := "站点"
	if lang == config.EnglishLanguage {
		label = "Website"
	}
	websiteName := strings.TrimSpace(query.WebsiteID)
	if website, ok := config.GetWebsiteByID(query.WebsiteID); ok {
		if name := strings.TrimSpace(website.Name); name != "" {
			websiteName = name
		}
	}
	if websiteName == "" {
		websiteName = "-"
	}
	return []string{label, websiteName}
}

func normalizeExportLang(lang string) string {
	normalized := config.NormalizeLanguage(lang)
	if normalized == "" {
		return config.GetLanguage()
	}
	return normalized
}
