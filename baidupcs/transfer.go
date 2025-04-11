package baidupcs

import (
	"fmt"
	"github.com/qjfoidnh/BaiduPCS-Go/pcsutil"
	"github.com/qjfoidnh/BaiduPCS-Go/requester"
	"github.com/tidwall/gjson"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"github.com/qjfoidnh/BaiduPCS-Go/baidupcs/pcserror"
)

type (
	// ShareOption 分享可选项
	TransferOption struct {
		Download bool   // 是否直接开始下载
		Collect  bool   // 多文件整合
		Rname    bool   // 随机改文件名
		Dir      string // 要转存的目录路径，空为根目录
		FsId     int64  // 要转存的特定文件ID，0为转存当前目录下所有文件
		SaveTo   string // 要转存到的目标目录路径
		Select   string // 选择需要转存的文件或目录，用英文逗号分割多个项目
	}
)

func (pcs *BaiduPCS) GenerateShareQueryURL(subPath string, params map[string]string) *url.URL {
	shareURL := &url.URL{
		Scheme: GetHTTPScheme(true),
		Host:   PanBaiduCom,
		Path:   "/share/" + subPath,
	}
	uv := shareURL.Query()
	for key, value := range params {
		uv.Set(key, value)
	}

	shareURL.RawQuery = uv.Encode()
	return shareURL
}

func (pcs *BaiduPCS) ExtractShareInfo(shareURL, shardID, shareUK, bdstoken string) (res map[string]string) {
	res = make(map[string]string)
	dataReadCloser, panError := pcs.sendReqReturnReadCloser(reqTypePan, OperationShareFileSavetoLocal, http.MethodGet, shareURL, nil, map[string]string{
		"User-Agent":   requester.UserAgent,
		"Content-Type": "application/x-www-form-urlencoded; charset=UTF-8",
	})
	if panError != nil {
		res["ErrMsg"] = "提交分享项查询请求时发生错误"
		return
	}
	defer dataReadCloser.Close()
	body, _ := ioutil.ReadAll(dataReadCloser)
	errno := gjson.Get(string(body), `errno`).Int()
	if errno != 0 {
		res["ErrMsg"] = fmt.Sprintf("未知错误, 错误码%d", errno)
		if errno == 8001 {
			res["ErrMsg"] = "已触发验证, 请稍后再试"
		}
		return
	}
	res["filename"] = gjson.Get(string(body), `list.0.server_filename`).String()
	fsidList := gjson.Get(string(body), `list.#.fs_id`).Array()
	var fidsStr string = "["
	for _, sid := range fsidList {
		fidsStr += sid.String() + ","
	}

	res["shareid"] = shardID
	res["from"] = shareUK
	res["bdstoken"] = bdstoken
	shareUrl := &url.URL{
		Scheme: GetHTTPScheme(true),
		Host:   PanBaiduCom,
		Path:   "/share/transfer",
	}
	uv := shareUrl.Query()
	uv.Set("app_id", PanAppID)
	uv.Set("channel", "chunlei")
	uv.Set("clienttype", "0")
	uv.Set("web", "1")
	for key, value := range res {
		uv.Set(key, value)
	}
	res["item_num"] = strconv.Itoa(len(fsidList))
	res["ErrMsg"] = "success"
	res["fs_id"] = fidsStr[:len(fidsStr)-1] + "]"
	shareUrl.RawQuery = uv.Encode()
	res["shareUrl"] = shareUrl.String()
	return
}

func (pcs *BaiduPCS) PostShareQuery(url string, referer string, data map[string]string) (res map[string]string) {
	dataReadCloser, panError := pcs.sendReqReturnReadCloser(reqTypePan, OperationShareFileSavetoLocal, http.MethodPost, url, data, map[string]string{
		"User-Agent":   requester.UserAgent,
		"Content-Type": "application/x-www-form-urlencoded; charset=UTF-8",
		"Referer":      referer,
	})
	res = make(map[string]string)
	if panError != nil {
		res["ErrMsg"] = "提交分享项查询请求时发生错误"
		return
	}
	defer dataReadCloser.Close()
	body, _ := ioutil.ReadAll(dataReadCloser)
	errno := gjson.Get(string(body), `errno`).Int()
	if errno != 0 {
		res["ErrMsg"] = fmt.Sprintf("未知错误, 错误码%d", errno)
		if errno == -9 {
			res["ErrMsg"] = "提取码错误"
		}
		return
	}
	res["randsk"] = gjson.Get(string(body), `randsk`).String()
	res["ErrMsg"] = "0"
	return
}

func (pcs *BaiduPCS) AccessSharePage(featurestr string, first bool) (tokens map[string]string) {
	tokens = make(map[string]string)
	tokens["ErrMsg"] = "0"
	headers := make(map[string]string)
	headers["User-Agent"] = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/76.0.3809.100 Safari/537.36"
	headers["Referer"] = "https://pan.baidu.com/disk/home"
	if !first {
		headers["Referer"] = fmt.Sprintf("https://pan.baidu.com/share/init?surl=%s", featurestr[1:])
	}
	shareLink := fmt.Sprintf("https://pan.baidu.com/s/%s", featurestr)

	dataReadCloser, panError := pcs.sendReqReturnReadCloser(reqTypePan, OperationShareFileSavetoLocal, http.MethodGet, shareLink, nil, headers)

	if panError != nil {
		tokens["ErrMsg"] = "访问分享页失败"
		return
	}
	defer dataReadCloser.Close()
	body, _ := ioutil.ReadAll(dataReadCloser)
	notFoundFlag := strings.Contains(string(body), "platform-non-found")
	errorPageTitle := strings.Contains(string(body), "error-404")
	if errorPageTitle {
		tokens["ErrMsg"] = "页面不存在"
		return
	}
	if notFoundFlag {
		tokens["ErrMsg"] = "分享链接已失效"
		return
	} else {
		re, _ := regexp.Compile(`(\{.+?loginstate.+?\})\);`)
		sub := re.FindSubmatch(body)
		if len(sub) < 2 {
			tokens["ErrMsg"] = "请确认登录参数中已经包含了网盘STOKEN"
			return
		}
		tokens["bdstoken"] = gjson.Get(string(sub[1]), `bdstoken`).String()
		tokens["uk"] = gjson.Get(string(sub[1]), `uk`).String()
		tokens["share_uk"] = gjson.Get(string(sub[1]), `share_uk`).String()
		tokens["shareid"] = gjson.Get(string(sub[1]), `shareid`).String()
		return
	}

}

func (pcs *BaiduPCS) GenerateRequestQuery(mode string, params map[string]string) (res map[string]string) {
	res = make(map[string]string)
	res["ErrNo"] = "0"
	headers := map[string]string{
		"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/76.0.3809.100 Safari/537.36",
		"Referer":    params["referer"],
	}
	if mode == "POST" {
		headers["Content-Type"] = "application/x-www-form-urlencoded"
	}
	postdata := make(map[string]string)
	postdata["fsidlist"] = params["fs_id"]
	postdata["path"] = params["path"]
	fmt.Println(postdata["path"])
	dataReadCloser, panError := pcs.sendReqReturnReadCloser(reqTypePan, OperationShareFileSavetoLocal, mode, params["shareUrl"], postdata, headers)
	if panError != nil {
		res["ErrNo"] = "1"
		res["ErrMsg"] = "网络错误"
		return
	}
	defer dataReadCloser.Close()
	body, err := ioutil.ReadAll(dataReadCloser)
	if err != nil {
		res["ErrNo"] = "-1"
		res["ErrMsg"] = "未知错误"
		return
	}
	if !gjson.Valid(string(body)) {
		res["ErrNo"] = "2"
		res["ErrMsg"] = "返回json解析错误"
		return
	}
	errno := gjson.Get(string(body), `errno`).Int()
	if errno != 0 {
		res["ErrNo"] = "3"
		res["ErrMsg"] = "获取分享项元数据错误"
		if mode == "POST" && errno == 12 {
			path := gjson.Get(string(body), `info.0.path`).String()
			_, file := filepath.Split(path) // Should be path.Split here, but never mind~
			_errno := gjson.Get(string(body), `info.0.errno`).Int()
			targetFileNums := gjson.Get(string(body), `target_file_nums`).Int()
			targetFileNumsLimit := gjson.Get(string(body), `target_file_nums_limit`).Int()
			if targetFileNums > targetFileNumsLimit {
				res["ErrNo"] = "4"
				res["ErrMsg"] = fmt.Sprintf("转存文件数%d超过当前用户上限, 当前用户单次最大转存数%d", targetFileNums, targetFileNumsLimit)
				res["limit"] = fmt.Sprintf("%d", targetFileNumsLimit)
			} else if _errno == -30 {
				res["ErrNo"] = "9"
				res["ErrMsg"] = fmt.Sprintf("当前目录下已有%s同名文件/文件夹", file)
			} else {
				res["ErrMsg"] = fmt.Sprintf("未知错误, 错误代码%d", _errno)
			}
		} else if mode == "POST" && errno == 4 {
			res["ErrMsg"] = fmt.Sprintf("文件重复")
		}
		return
	}

	_, res["filename"] = filepath.Split(gjson.Get(string(body), `info.0.path`).String())
	filenames := gjson.Get(string(body), `info.#.path`).Array()
	filenamesStr := ""
	for _, _path := range filenames {
		filenamesStr += "," + path.Base(_path.String())
	}
	if len(filenamesStr) < 1 {
		res["filenames"] = "default" + pcsutil.GenerateRandomString(5)
	} else {
		res["filenames"] = filenamesStr[1:]
	}
	if len(gjson.Get(string(body), `info.#.fsid`).Array()) > 1 {
		res["filename"] += "等多个文件/文件夹"
	}
	return
}

func (pcs *BaiduPCS) SuperTransfer(params map[string]string, limit string) {
	//headers := map[string]string{
	//	"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/76.0.3809.100 Safari/537.36",
	//	"Referer":    params["referer"],
	//}
	//limit_num, _ := strconv.Atoi(limit)
	//fsidlist_str := params["fs_id"]
	//fsidlist := strings.Split(fsidlist_str[1:len(fsidlist_str)-1], ",")
	//listUrl := &url.URL{
	//	Scheme: GetHTTPScheme(true),
	//	Host:   PanBaiduCom,
	//	Path:   "/share/list",
	//}
	//uv := listUrl.Query()
	//uv.Set("app_id", PanAppID)
	//uv.Set("channel", "chunlei")
	//uv.Set("clienttype", "0")
	//uv.Set("web", "1")
	//uv.Set("page", "1")
	//uv.Set("num", "100")
	//uv.Set("shorturl", params["shorturl"])
	//uv.Set("root", "1")
	//dataReadCloser, panError := pcs.sendReqReturnReadCloser(reqTypePan, OperationShareFileSavetoLocal, http.MethodGet, listUrl.String(), nil, headers)
	//if panError != nil {
	//	res["ErrNo"] = "1"
	//	res["ErrMsg"] = "网络错误"
	//	return
	//}
	//defer dataReadCloser.Close()
	//body, err := ioutil.ReadAll(dataReadCloser)
	//res["ErrNo"] = "-1"
	//if err != nil {
	//	res["ErrMsg"] = "未知错误"
	//	return
	//}
	return

}

// GetShareDirList 获取分享链接中特定目录下的文件列表
func (pcs *BaiduPCS) GetShareDirList(featureStr, dir, shareid, shareUK, bdstoken string) (res map[string]string) {
	res = make(map[string]string)
	
	// 调整目录路径格式
	dir = strings.TrimSpace(dir)
	if dir != "" && !strings.HasPrefix(dir, "/") {
		dir = "/" + dir
	}
	
	featureMap := map[string]string{
		"bdstoken": bdstoken,
		"web":      "5",
		"app_id":   PanAppID,
		"shorturl": featureStr[1:],
		"channel":  "chunlei",
	}
	
	// 如果是根目录，则使用root=1
	if dir == "" || dir == "/" {
		featureMap["root"] = "1"
	} else {
		// 尝试直接使用路径
		featureMap["root"] = "0" // 子目录使用root=0
		featureMap["dir"] = dir
	}
	
	// 构建查询URL
	queryShareInfoUrl := pcs.GenerateShareQueryURL("list", featureMap).String()
	fmt.Printf("获取目录 '%s' 内容，URL: %s\n", dir, queryShareInfoUrl)
	
	// 获取目录列表
	dataReadCloser, panError := pcs.sendReqReturnReadCloser(reqTypePan, OperationShareFileSavetoLocal, http.MethodGet, queryShareInfoUrl, nil, map[string]string{
		"User-Agent":   requester.UserAgent,
		"Content-Type": "application/x-www-form-urlencoded; charset=UTF-8",
	})
	if panError != nil {
		res["ErrMsg"] = "提交分享项查询请求时发生错误"
		return
	}
	defer dataReadCloser.Close()
	
	body, _ := ioutil.ReadAll(dataReadCloser)
	errno := gjson.Get(string(body), `errno`).Int()
	if errno != 0 {
		// 如果查询目录失败，尝试查找此目录是否存在于根目录中
		if dir != "/" && dir != "" {
			fmt.Printf("直接查询目录'%s'失败 (错误码%d)，尝试在共享目录中查找...\n", dir, errno)
			
			// 查询根目录
			rootResult := pcs.GetShareDirList(featureStr, "/", shareid, shareUK, bdstoken)
			if rootResult["ErrMsg"] != "success" {
				res["ErrMsg"] = fmt.Sprintf("获取目录'%s'失败, 错误码%d", dir, errno)
				fmt.Printf("获取目录列表失败，响应内容: %s\n", string(body))
				return
			}
			
			// 获取根目录列表成功，尝试查找目标目录
			rootUrl := pcs.GenerateShareQueryURL("list", map[string]string{
				"bdstoken": bdstoken,
				"root":     "1",
				"web":      "5",
				"app_id":   PanAppID,
				"shorturl": featureStr[1:],
				"channel":  "chunlei",
			}).String()
			
			rootDataReadCloser, rootPanError := pcs.sendReqReturnReadCloser(reqTypePan, OperationShareFileSavetoLocal, http.MethodGet, rootUrl, nil, map[string]string{
				"User-Agent":   requester.UserAgent,
				"Content-Type": "application/x-www-form-urlencoded; charset=UTF-8",
			})
			if rootPanError != nil {
				res["ErrMsg"] = "查询根目录失败"
				return
			}
			
			rootBody, _ := ioutil.ReadAll(rootDataReadCloser)
			rootDataReadCloser.Close()
			
			// 提取文件列表
			rootFiles := gjson.Get(string(rootBody), `list`).Array()
			fmt.Printf("根目录中找到 %d 个文件/目录\n", len(rootFiles))
			
			// 获取目标路径的最后部分
			dirParts := strings.Split(strings.Trim(dir, "/"), "/")
			targetDirName := ""
			if len(dirParts) > 0 {
				targetDirName = dirParts[len(dirParts)-1]
			}
			
			// 在根目录中查找目标目录
			var matchedDirPath string
			for _, file := range rootFiles {
				filePath := file.Get("path").String()
				fileName := file.Get("server_filename").String()
				isDir := file.Get("isdir").Int() == 1
				
				fmt.Printf("检查: %s (路径: %s, 是目录: %v)\n", fileName, filePath, isDir)
				
				// 如果是目标目录或路径包含目标目录
				if (isDir && fileName == targetDirName) || strings.Contains(filePath, dir) {
					matchedDirPath = filePath
					fmt.Printf("找到可能匹配的目录: %s\n", matchedDirPath)
					break
				}
				
				// 检查解码后的路径
				decodedFilePath := decodeUnicode(filePath)
				decodedDirName := decodeUnicode(targetDirName)
				decodedDir := decodeUnicode(dir)
				
				if (isDir && strings.Contains(fileName, targetDirName)) || 
				   strings.Contains(decodedFilePath, decodedDir) ||
				   strings.Contains(decodedFilePath, decodedDirName) {
					matchedDirPath = filePath
					fmt.Printf("使用解码比较找到可能匹配的目录: %s\n", matchedDirPath)
					break
				}
			}
			
			// 如果找到了匹配的目录路径，重新查询
			if matchedDirPath != "" {
				fmt.Printf("使用找到的路径重新查询: %s\n", matchedDirPath)
				
				// 构建新的查询URL
				retryFeatureMap := map[string]string{
					"bdstoken": bdstoken,
					"root":     "0",
					"web":      "5",
					"app_id":   PanAppID,
					"shorturl": featureStr[1:],
					"channel":  "chunlei",
					"dir":      matchedDirPath,
				}
				
				retryQueryUrl := pcs.GenerateShareQueryURL("list", retryFeatureMap).String()
				
				// 重新查询
				retryDataReadCloser, retryPanError := pcs.sendReqReturnReadCloser(reqTypePan, OperationShareFileSavetoLocal, http.MethodGet, retryQueryUrl, nil, map[string]string{
					"User-Agent":   requester.UserAgent,
					"Content-Type": "application/x-www-form-urlencoded; charset=UTF-8",
				})
				if retryPanError != nil {
					res["ErrMsg"] = "重新查询目录失败"
					return
				}
				
				// 读取响应
				retryBody, _ := ioutil.ReadAll(retryDataReadCloser)
				retryDataReadCloser.Close()
				
				retryErrno := gjson.Get(string(retryBody), `errno`).Int()
				if retryErrno != 0 {
					res["ErrMsg"] = fmt.Sprintf("重新查询目录'%s'失败, 错误码%d", matchedDirPath, retryErrno)
					fmt.Printf("重新查询失败，响应内容: %s\n", string(retryBody))
					return
				}
				
				// 使用重新查询的结果
				body = retryBody
			} else {
				res["ErrMsg"] = fmt.Sprintf("在共享链接中未找到目录'%s'", dir)
				return
			}
		} else {
			res["ErrMsg"] = fmt.Sprintf("获取目录列表失败，错误码%d", errno)
			fmt.Printf("获取目录列表失败，响应内容: %s\n", string(body))
			if errno == 8001 {
				res["ErrMsg"] = "已触发验证, 请稍后再试"
			}
			return
		}
	}
	
	// 提取文件列表信息
	filesData := gjson.Get(string(body), `list`).Array()
	fmt.Printf("成功获取目录 '%s' 的内容，共 %d 个文件/目录\n", dir, len(filesData))
	
	if len(filesData) == 0 {
		res["ErrMsg"] = "目录为空或不存在"
		return
	}
	
	// 处理文件列表
	var fidsStr string = "["
	for _, file := range filesData {
		if file.Get("fs_id").Exists() {
			fidsStr += file.Get("fs_id").String() + ","
			fmt.Printf("  - %s (ID: %d, 路径: %s, 是目录: %v)\n", 
				file.Get("server_filename").String(),
				file.Get("fs_id").Int(),
				file.Get("path").String(),
				file.Get("isdir").Int() == 1)
		}
	}
	
	if len(fidsStr) == 1 { // 只有"["，没有文件
		res["ErrMsg"] = "目录为空或不存在"
		return
	}
	
	res["shareid"] = shareid
	res["from"] = shareUK
	res["bdstoken"] = bdstoken
	res["filename"] = gjson.Get(string(body), `list.0.server_filename`).String()
	res["item_num"] = strconv.Itoa(len(filesData))
	res["ErrMsg"] = "success"
	res["fs_id"] = fidsStr[:len(fidsStr)-1] + "]" // 去除最后一个逗号
	
	// 构建转存用URL
	shareUrl := &url.URL{
		Scheme: GetHTTPScheme(true),
		Host:   PanBaiduCom,
		Path:   "/share/transfer",
	}
	uv := shareUrl.Query()
	uv.Set("app_id", PanAppID)
	uv.Set("channel", "chunlei")
	uv.Set("clienttype", "0")
	uv.Set("web", "1")
	for key, value := range res {
		uv.Set(key, value)
	}
	shareUrl.RawQuery = uv.Encode()
	res["shareUrl"] = shareUrl.String()
	
	return
}

// GetShareFileByFsId 获取分享链接中特定ID的文件信息
func (pcs *BaiduPCS) GetShareFileByFsId(featureStr, dir, shareid, shareUK, bdstoken string, fsId int64) (res map[string]string) {
	// 首先获取目录下的所有文件列表
	res = pcs.GetShareDirList(featureStr, dir, shareid, shareUK, bdstoken)
	if res["ErrMsg"] != "success" {
		return
	}
	
	// 获取目录列表
	queryShareInfoUrl := pcs.GenerateShareQueryURL("list", map[string]string{
		"bdstoken": bdstoken,
		"root":     "0",
		"web":      "5",
		"app_id":   PanAppID,
		"shorturl": featureStr[1:],
		"channel":  "chunlei",
		"dir":      dir,
	}).String()
	
	dataReadCloser, panError := pcs.sendReqReturnReadCloser(reqTypePan, OperationShareFileSavetoLocal, http.MethodGet, queryShareInfoUrl, nil, map[string]string{
		"User-Agent":   requester.UserAgent,
		"Content-Type": "application/x-www-form-urlencoded; charset=UTF-8",
	})
	if panError != nil {
		res["ErrMsg"] = "提交分享项查询请求时发生错误"
		return
	}
	defer dataReadCloser.Close()
	
	body, _ := ioutil.ReadAll(dataReadCloser)
	errno := gjson.Get(string(body), `errno`).Int()
	if errno != 0 {
		res["ErrMsg"] = fmt.Sprintf("未知错误, 错误码%d", errno)
		return
	}
	
	// 查找特定ID的文件
	foundFile := false
	filesData := gjson.Get(string(body), `list`).Array()
	for _, file := range filesData {
		if file.Get("fs_id").Int() == fsId {
			foundFile = true
			res["fs_id"] = fmt.Sprintf("[%d]", fsId)
			res["filename"] = file.Get("server_filename").String()
			res["item_num"] = "1"
			break
		}
	}
	
	if !foundFile {
		res["ErrMsg"] = fmt.Sprintf("在目录中未找到ID为 %d 的文件", fsId)
		return
	}
	
	// 构建转存用URL
	shareUrl := &url.URL{
		Scheme: GetHTTPScheme(true),
		Host:   PanBaiduCom,
		Path:   "/share/transfer",
	}
	uv := shareUrl.Query()
	uv.Set("app_id", PanAppID)
	uv.Set("channel", "chunlei")
	uv.Set("clienttype", "0")
	uv.Set("web", "1")
	for key, value := range res {
		uv.Set(key, value)
	}
	shareUrl.RawQuery = uv.Encode()
	res["shareUrl"] = shareUrl.String()
	
	return
}

// GetShareFileByPath 根据路径自动查找并获取共享链接中特定文件的信息
func (pcs *BaiduPCS) GetShareFileByPath(featureStr, targetPath, shareid, shareUK, bdstoken string) (res map[string]string) {
	// 路径处理：确保格式统一
	if !strings.HasPrefix(targetPath, "/") {
		targetPath = "/" + targetPath
	}
	
	// 分割路径
	pathParts := strings.Split(strings.Trim(targetPath, "/"), "/")
	if len(pathParts) == 0 {
		// 如果是根目录，直接获取根目录列表
		return pcs.GetShareDirList(featureStr, "/", shareid, shareUK, bdstoken)
	}
	
	// 创建返回结果
	res = make(map[string]string)
	
	// 先尝试获取父目录内容，而不是直接尝试获取目标文件
	// 例如：对于 /发货/股票/level2/win/2017-2024/2025/202504/20250407.7z
	// 先尝试获取 /发货/股票/level2/win/2017-2024/2025/202504/ 目录内容
	
	// 获取目标文件名和父目录路径
	targetFile := pathParts[len(pathParts)-1]  // 文件名
	parentDirPath := ""
	if len(pathParts) > 1 {
		parentDirPath = "/" + strings.Join(pathParts[:len(pathParts)-1], "/")
	}
	
	fmt.Printf("目标文件名: %s, 父目录路径: %s\n", targetFile, parentDirPath)
	
	// 首先尝试直接获取父目录内容
	if parentDirPath != "" {
		fmt.Printf("尝试获取父目录: %s 的内容\n", parentDirPath)
		parentDir := pcs.GetShareDirList(featureStr, parentDirPath, shareid, shareUK, bdstoken)
		
		if parentDir["ErrMsg"] == "success" {
			// 成功获取父目录，现在查找目标文件
			fmt.Printf("成功获取父目录内容，尝试查找目标文件: %s\n", targetFile)
			
			// 获取父目录内容详情
			dirUrl := pcs.GenerateShareQueryURL("list", map[string]string{
				"bdstoken": bdstoken,
				"root":     "0",
				"web":      "5",
				"app_id":   PanAppID,
				"shorturl": featureStr[1:],
				"channel":  "chunlei",
				"dir":      parentDirPath,
			}).String()
			
			fmt.Printf("获取父目录详情: %s\n", dirUrl)
			
			dirDataCloser, dirError := pcs.sendReqReturnReadCloser(reqTypePan, OperationShareFileSavetoLocal, http.MethodGet, dirUrl, nil, map[string]string{
				"User-Agent":   requester.UserAgent,
				"Content-Type": "application/x-www-form-urlencoded; charset=UTF-8",
			})
			
			if dirError == nil {
				dirData, _ := ioutil.ReadAll(dirDataCloser)
				dirDataCloser.Close()
				
				dirErrno := gjson.Get(string(dirData), `errno`).Int()
				if dirErrno == 0 {
					// 成功获取父目录详情
					dirFiles := gjson.Get(string(dirData), `list`).Array()
					fmt.Printf("在父目录中找到 %d 个文件/目录\n", len(dirFiles))
					
					// 准备解码比较用的目标文件名
					decodedTargetFile := decodeUnicode(targetFile)
					
					// 查找目标文件
					for _, dirFile := range dirFiles {
						curFileName := dirFile.Get("server_filename").String()
						isDir := dirFile.Get("isdir").Int() == 1
						
						// 解码当前文件名用于比较
						decodedFileName := decodeUnicode(curFileName)
						
						fmt.Printf("  - 文件: %s (解码后: %s, 是目录: %v)\n", 
							curFileName, decodedFileName, isDir)
						
						// 多种匹配方式：精确匹配、解码后匹配、部分包含
						if curFileName == targetFile || 
						   decodedFileName == decodedTargetFile || 
						   strings.Contains(curFileName, targetFile) ||
						   strings.Contains(decodedFileName, decodedTargetFile) {
							// 找到目标文件
							fsId := dirFile.Get("fs_id").Int()
							if isDir {
								fmt.Printf("在父目录中找到目标目录: %s, FSID: %d\n", curFileName, fsId)
							} else {
								fmt.Printf("在父目录中找到目标文件: %s, FSID: %d\n", curFileName, fsId)
							}
							
							// 创建结果
							res["shareid"] = shareid
							res["from"] = shareUK
							res["bdstoken"] = bdstoken
							res["filename"] = curFileName
							res["item_num"] = "1"
							res["ErrMsg"] = "success"
							res["fs_id"] = fmt.Sprintf("[%d]", fsId)
							
							// 构建转存用URL
							shareUrl := &url.URL{
								Scheme: GetHTTPScheme(true),
								Host:   PanBaiduCom,
								Path:   "/share/transfer",
							}
							uv := shareUrl.Query()
							uv.Set("app_id", PanAppID)
							uv.Set("channel", "chunlei")
							uv.Set("clienttype", "0")
							uv.Set("web", "1")
							for key, value := range res {
								uv.Set(key, value)
							}
							shareUrl.RawQuery = uv.Encode()
							res["shareUrl"] = shareUrl.String()
							
							return res
						}
					}
					
					fmt.Printf("在父目录中未找到目标文件: %s\n", targetFile)
				} else {
					fmt.Printf("获取父目录详情失败，错误码: %d\n", dirErrno)
				}
			} else {
				fmt.Printf("请求父目录详情失败: %s\n", dirError.Error())
			}
		} else {
			fmt.Printf("获取父目录失败: %s\n", parentDir["ErrMsg"])
		}
	}
	
	// 如果通过父目录找不到，尝试直接访问完整路径
	fmt.Printf("尝试直接获取目标路径: %s\n", targetPath)
	directPath := pcs.GetShareDirList(featureStr, targetPath, shareid, shareUK, bdstoken)
	if directPath["ErrMsg"] == "success" {
		// 如果成功获取到了目标路径，直接返回结果
		fmt.Printf("成功直接获取目标路径: %s\n", targetPath)
		return directPath
	} else {
		fmt.Printf("直接获取目标路径失败: %s\n", directPath["ErrMsg"])
	}
	
	// 上面的方法都失败了，现在尝试获取根目录然后自己查找
	// 先获取根目录文件列表
	queryShareInfoUrl := pcs.GenerateShareQueryURL("list", map[string]string{
		"bdstoken": bdstoken,
		"root":     "1",
		"web":      "5",
		"app_id":   PanAppID,
		"shorturl": featureStr[1:],
		"channel":  "chunlei",
	}).String()
	
	fmt.Printf("查询根目录列表, URL: %s\n", queryShareInfoUrl)
	
	dataReadCloser, panError := pcs.sendReqReturnReadCloser(reqTypePan, OperationShareFileSavetoLocal, http.MethodGet, queryShareInfoUrl, nil, map[string]string{
		"User-Agent":   requester.UserAgent,
		"Content-Type": "application/x-www-form-urlencoded; charset=UTF-8",
	})
	if panError != nil {
		res["ErrMsg"] = "提交分享项查询请求时发生错误"
		return res
	}
	
	body, _ := ioutil.ReadAll(dataReadCloser)
	dataReadCloser.Close()
	
	errno := gjson.Get(string(body), `errno`).Int()
	if errno != 0 {
		res["ErrMsg"] = fmt.Sprintf("获取根目录失败, 错误码%d", errno)
		// 打印响应体帮助调试
		fmt.Printf("返回内容: %s\n", string(body))
		return res
	}
	
	// 获取文件列表
	filesData := gjson.Get(string(body), `list`).Array()
	fmt.Printf("根目录中找到 %d 个文件/目录\n", len(filesData))
	
	// 先尝试直接在根目录中查找目标文件
	decodedTargetFile := decodeUnicode(targetFile)
	
	for _, file := range filesData {
		fileName := file.Get("server_filename").String()
		decodedFileName := decodeUnicode(fileName)
		
		// 查看是否直接找到目标文件
		if fileName == targetFile || decodedFileName == decodedTargetFile {
			fsId := file.Get("fs_id").Int()
			isDir := file.Get("isdir").Int() == 1
			
			fileType := "文件"
			if isDir {
				fileType = "目录"
			}
			
			fmt.Printf("在根目录中直接找到目标%s: %s, FSID: %d\n", fileType, fileName, fsId)
			
			// 创建结果
			res["shareid"] = shareid
			res["from"] = shareUK
			res["bdstoken"] = bdstoken
			res["filename"] = fileName
			res["item_num"] = "1"
			res["ErrMsg"] = "success"
			res["fs_id"] = fmt.Sprintf("[%d]", fsId)
			
			// 构建转存用URL
			shareUrl := &url.URL{
				Scheme: GetHTTPScheme(true),
				Host:   PanBaiduCom,
				Path:   "/share/transfer",
			}
			uv := shareUrl.Query()
			uv.Set("app_id", PanAppID)
			uv.Set("channel", "chunlei")
			uv.Set("clienttype", "0")
			uv.Set("web", "1")
			for key, value := range res {
				uv.Set(key, value)
			}
			shareUrl.RawQuery = uv.Encode()
			res["shareUrl"] = shareUrl.String()
			
			return res
		}
	}
	
	// 如果根目录中没有找到目标文件，则尝试逐级查找目录
	// 构建从根级开始的逐级路径
	var currentPath string
	for i, part := range pathParts {
		// 逐级构建路径
		if i == 0 {
			currentPath = "/" + part
		} else {
			currentPath = currentPath + "/" + part
		}
		
		fmt.Printf("尝试查找路径: %s\n", currentPath)
		
		// 获取当前级别目录
		dirContent := pcs.GetShareDirList(featureStr, currentPath, shareid, shareUK, bdstoken)
		
		// 如果成功获取到当前级别，并且已经到达最后一级，则直接返回结果
		if dirContent["ErrMsg"] == "success" && i == len(pathParts)-1 {
			fmt.Printf("成功找到目标路径: %s\n", currentPath)
			return dirContent
		}
		
		// 如果当前级别获取成功，但还需要继续下一级，则继续循环
		if dirContent["ErrMsg"] != "success" {
			fmt.Printf("获取路径 %s 失败: %s\n", currentPath, dirContent["ErrMsg"])
			break
		}
	}
	
	// 如果前面的方法都失败，尝试深度搜索所有目录
	fmt.Println("尝试深度搜索所有目录...")
	
	for _, file := range filesData {
		isDir := file.Get("isdir").Int() == 1
		if isDir {
			filePath := file.Get("path").String()
			fileName := file.Get("server_filename").String()
			
			fmt.Printf("深度搜索目录: %s (%s)\n", fileName, filePath)
			
			// 检查当前目录是否为目标目录
			if fileName == targetFile {
				// 找到了目标目录，直接使用它
				fsId := file.Get("fs_id").Int()
				fmt.Printf("在深度搜索中找到匹配的目标目录: %s, FSID: %d\n", fileName, fsId)
				
				// 创建结果
				res["shareid"] = shareid
				res["from"] = shareUK
				res["bdstoken"] = bdstoken
				res["filename"] = fileName
				res["item_num"] = "1"
				res["ErrMsg"] = "success"
				res["fs_id"] = fmt.Sprintf("[%d]", fsId)
				
				// 构建转存用URL
				shareUrl := &url.URL{
					Scheme: GetHTTPScheme(true),
					Host:   PanBaiduCom,
					Path:   "/share/transfer",
				}
				uv := shareUrl.Query()
				uv.Set("app_id", PanAppID)
				uv.Set("channel", "chunlei")
				uv.Set("clienttype", "0")
				uv.Set("web", "1")
				for key, value := range res {
					uv.Set(key, value)
				}
				shareUrl.RawQuery = uv.Encode()
				res["shareUrl"] = shareUrl.String()
				
				return res
			}
			
			// 获取该目录下的文件列表
			dirUrl := pcs.GenerateShareQueryURL("list", map[string]string{
				"bdstoken": bdstoken,
				"root":     "0",
				"web":      "5",
				"app_id":   PanAppID,
				"shorturl": featureStr[1:],
				"channel":  "chunlei",
				"dir":      filePath,
			}).String()
			
			fmt.Printf("查询目录内容: %s\n", dirUrl)
			
			dirDataCloser, dirError := pcs.sendReqReturnReadCloser(reqTypePan, OperationShareFileSavetoLocal, http.MethodGet, dirUrl, nil, map[string]string{
				"User-Agent":   requester.UserAgent,
				"Content-Type": "application/x-www-form-urlencoded; charset=UTF-8",
			})
			
			if dirError == nil {
				dirData, _ := ioutil.ReadAll(dirDataCloser)
				dirDataCloser.Close()
				
				dirErrno := gjson.Get(string(dirData), `errno`).Int()
				if dirErrno == 0 {
					// 成功获取目录内容
					dirFiles := gjson.Get(string(dirData), `list`).Array()
					fmt.Printf("在目录 %s 中找到 %d 个文件\n", fileName, len(dirFiles))
					
					// 解码目标文件名用于比较
					decodedTargetFile := decodeUnicode(targetFile)
					
					// 查找目标文件
					for _, dirFile := range dirFiles {
						curFileName := dirFile.Get("server_filename").String()
						isDir := dirFile.Get("isdir").Int() == 1
						
						// 解码当前文件名用于比较
						decodedFileName := decodeUnicode(curFileName)
						
						// 多种匹配方式：精确匹配、解码后匹配、部分包含
						if curFileName == targetFile || 
						   decodedFileName == decodedTargetFile || 
						   strings.Contains(curFileName, targetFile) ||
						   strings.Contains(decodedFileName, decodedTargetFile) {
							// 找到目标文件/目录
							fsId := dirFile.Get("fs_id").Int()
							if isDir {
								fmt.Printf("在深度搜索中找到目标目录: %s, FSID: %d\n", curFileName, fsId)
							} else {
								fmt.Printf("在深度搜索中找到目标文件: %s, FSID: %d\n", curFileName, fsId)
							}
							
							// 创建结果
							res["shareid"] = shareid
							res["from"] = shareUK
							res["bdstoken"] = bdstoken
							res["filename"] = curFileName
							res["item_num"] = "1"
							res["ErrMsg"] = "success"
							res["fs_id"] = fmt.Sprintf("[%d]", fsId)
							
							// 构建转存用URL
							shareUrl := &url.URL{
								Scheme: GetHTTPScheme(true),
								Host:   PanBaiduCom,
								Path:   "/share/transfer",
							}
							uv := shareUrl.Query()
							uv.Set("app_id", PanAppID)
							uv.Set("channel", "chunlei")
							uv.Set("clienttype", "0")
							uv.Set("web", "1")
							for key, value := range res {
								uv.Set(key, value)
							}
							shareUrl.RawQuery = uv.Encode()
							res["shareUrl"] = shareUrl.String()
							
							return res
						}
					}
				}
			}
		}
	}
	
	// 如果没有找到，返回错误
	res["ErrMsg"] = fmt.Sprintf("无法找到路径: %s", targetPath)
	return res
}

// decodeUnicode 解码Unicode编码的字符串
func decodeUnicode(s string) string {
	// 使用Go语言标准库处理Unicode转义序列
	var result strings.Builder
	i := 0
	for i < len(s) {
		if i+5 < len(s) && s[i:i+2] == "\\u" {
			// 处理 \uXXXX 格式的Unicode编码
			hexStr := s[i+2 : i+6]
			if val, err := strconv.ParseInt(hexStr, 16, 32); err == nil {
				result.WriteRune(rune(val))
				i += 6
				continue
			}
		}
		result.WriteByte(s[i])
		i++
	}
	return result.String()
}

// comparePathNames 比较两个路径名称，考虑到Unicode编码的问题
func comparePathNames(name1, name2 string) bool {
	// 标准比较
	if name1 == name2 {
		return true
	}
	
	// 解码后比较
	decoded1 := decodeUnicode(name1)
	decoded2 := decodeUnicode(name2)
	
	if decoded1 == decoded2 {
		return true
	}
	
	// 提取路径最后部分比较
	parts1 := strings.Split(decoded1, "/")
	parts2 := strings.Split(decoded2, "/")
	
	if len(parts1) > 0 && len(parts2) > 0 {
		return parts1[len(parts1)-1] == parts2[len(parts2)-1]
	}
	
	return false
}

// SendReqReturnReadCloser 包装sendReqReturnReadCloser方法，提供给外部包使用
func (pcs *BaiduPCS) SendReqReturnReadCloser(rt int, op, method, urlStr string, post interface{}, header map[string]string) (readCloser io.ReadCloser, pcsError pcserror.Error) {
	return pcs.sendReqReturnReadCloser(reqType(rt), op, method, urlStr, post, header)
}

// TransferShareFile 转存分享文件，支持指定目标目录
func (pcs *BaiduPCS) TransferShareFile(res map[string]string, saveTo string) (transferResult map[string]string) {
	transferResult = make(map[string]string)
	transferResult["ErrMsg"] = "success"
	
	// 检查是否已经有转存URL
	if _, ok := res["shareUrl"]; !ok {
		transferResult["ErrMsg"] = "无效的转存信息"
		return
	}
	
	// 解析现有URL
	shareUrl, err := url.Parse(res["shareUrl"])
	if err != nil {
		transferResult["ErrMsg"] = "URL解析错误"
		return
	}
	
	// 获取当前查询参数
	uv := shareUrl.Query()
	
	// 如果指定了目标目录，添加到查询参数中
	if saveTo != "" {
		// 确保目标目录以/开头
		if !strings.HasPrefix(saveTo, "/") {
			saveTo = "/" + saveTo
		}
		uv.Set("path", saveTo)
		fmt.Printf("指定转存到目标目录: %s\n", saveTo)
	}
	
	// 更新URL查询参数
	shareUrl.RawQuery = uv.Encode()
	
	// 构建POST请求
	postData := map[string]string{
		"fsidlist": res["fs_id"],
	}
	if saveTo != "" {
		postData["path"] = saveTo
	}
	
	// 发送转存请求
	dataReadCloser, panError := pcs.sendReqReturnReadCloser(reqTypePan, OperationShareFileSavetoLocal, http.MethodPost, shareUrl.String(), postData, map[string]string{
		"User-Agent":   requester.UserAgent,
		"Content-Type": "application/x-www-form-urlencoded; charset=UTF-8",
		"Referer":      "https://pan.baidu.com/",
	})
	
	if panError != nil {
		transferResult["ErrMsg"] = "提交转存请求时发生错误: " + panError.Error()
		return
	}
	defer dataReadCloser.Close()
	
	body, _ := ioutil.ReadAll(dataReadCloser)
	errno := gjson.Get(string(body), `errno`).Int()
	if errno != 0 {
		// 处理错误情况
		transferResult["ErrMsg"] = fmt.Sprintf("转存失败，错误码: %d", errno)
		
		// 特殊错误处理
		if errno == 12 {
			// 处理目录已存在等情况
			pathErr := gjson.Get(string(body), `info.0.errno`).Int()
			if pathErr == -30 {
				transferResult["ErrMsg"] = "文件重复"
			} else if pathErr != 0 {
				transferResult["ErrMsg"] = fmt.Sprintf("文件系统错误，错误码: %d", pathErr)
			}
		} else if errno == 4 {
			transferResult["ErrMsg"] = "文件重复"
		} else if errno == 110 {
			transferResult["ErrMsg"] = "请先登录"
		} else if errno == -7 {
			transferResult["ErrMsg"] = "转存功能被禁用"
		}
		
		return
	}
	
	// 转存成功
	transferResult["ErrMsg"] = "success"
	return
}
