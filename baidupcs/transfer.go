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
	
	// 先获取根目录文件列表
	queryShareInfoUrl := pcs.GenerateShareQueryURL("list", map[string]string{
		"bdstoken": bdstoken,
		"root":     "1",
		"web":      "5",
		"app_id":   PanAppID,
		"shorturl": featureStr[1:],
		"channel":  "chunlei",
	}).String()
	
	fmt.Printf("查询根目录列表: URL: %s\n", queryShareInfoUrl)
	
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
	fmt.Printf("找到 %d 个文件/目录\n", len(filesData))
	
	// 获取目标文件名和父目录路径
	targetFile := pathParts[len(pathParts)-1]  // 文件名
	parentDirPath := ""
	if len(pathParts) > 1 {
		parentDirPath = "/" + strings.Join(pathParts[:len(pathParts)-1], "/")
	}
	
	fmt.Printf("目标文件名: %s, 父目录路径: %s\n", targetFile, parentDirPath)
	
	// 首先，尝试获取父目录的内容
	parentDirFound := false
	
	if len(pathParts) > 1 {
		// 查找最接近的父目录
		for _, file := range filesData {
			filePath := file.Get("path").String()
			fileName := file.Get("server_filename").String()
			isDir := file.Get("isdir").Int() == 1
			
			fmt.Printf("  - 条目: %s (路径: %s, 是目录: %v)\n", fileName, filePath, isDir)
			
			// 如果发现共享链接中的路径与我们的目标路径相关，尝试找到最接近的父目录
			if isDir && (strings.Contains(decodeUnicode(filePath), decodeUnicode(parentDirPath))) {
				fmt.Printf("发现目标路径的可能父目录: %s\n", filePath)
				
				// 检查如果这个目录的名称与我们要找的目标名称匹配，则直接使用这个目录
				fileNameParts := strings.Split(filePath, "/")
				fileName := ""
				if len(fileNameParts) > 0 {
					fileName = fileNameParts[len(fileNameParts)-1]
				}
				
				if fileName == targetFile {
					// 找到了完全匹配的目录，直接使用它
					fsId := file.Get("fs_id").Int()
					fmt.Printf("找到完全匹配的目标目录: %s, FSID: %d\n", fileName, fsId)
					
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
				
				// 尝试直接访问父目录的内容
				parentDirContent := pcs.GetShareDirList(featureStr, filePath, shareid, shareUK, bdstoken)
				if parentDirContent["ErrMsg"] == "success" {
					fmt.Printf("成功获取目录内容，寻找目标文件: %s\n", targetFile)
					parentDirFound = true
					
					// 从 parentDirContent 中查找目标文件
					fsIds := strings.Trim(parentDirContent["fs_id"], "[]")
					if fsIds != "" {
						// 这是一个需要解析的文件ID列表
						fmt.Printf("尝试在目录中查找目标文件\n")
						
						// 在父目录下查询文件列表
						parentDirUrl := pcs.GenerateShareQueryURL("list", map[string]string{
							"bdstoken": bdstoken,
							"root":     "0",
							"web":      "5",
							"app_id":   PanAppID,
							"shorturl": featureStr[1:],
							"channel":  "chunlei",
							"dir":      filePath,
						}).String()
						
						parentDirDataCloser, parentDirError := pcs.sendReqReturnReadCloser(reqTypePan, OperationShareFileSavetoLocal, http.MethodGet, parentDirUrl, nil, map[string]string{
							"User-Agent":   requester.UserAgent,
							"Content-Type": "application/x-www-form-urlencoded; charset=UTF-8",
						})
						
						if parentDirError == nil {
							parentDirData, _ := ioutil.ReadAll(parentDirDataCloser)
							parentDirDataCloser.Close()
							
							parentDirErrno := gjson.Get(string(parentDirData), `errno`).Int()
							if parentDirErrno == 0 {
								parentDirFiles := gjson.Get(string(parentDirData), `list`).Array()
								fmt.Printf("父目录中有 %d 个文件/目录\n", len(parentDirFiles))
								
								// 在父目录中查找目标文件
								for _, dirFile := range parentDirFiles {
									fileName := dirFile.Get("server_filename").String()
									decodedFileName := decodeUnicode(fileName)
									decodedTargetFile := decodeUnicode(targetFile)
									
									// 修复三元运算符语法
									dirType := ""
									if isDir {
										dirType = ", 类型: 目录"
									}
									fmt.Printf("  检查文件: %s (%s)%s\n", 
										fileName, decodedFileName, dirType)
									
									// 检查是否找到了目标目录
									if (fileName == targetFile || 
									   decodedFileName == decodedTargetFile || 
									   strings.Contains(fileName, targetFile) ||
									   strings.Contains(decodedFileName, decodedTargetFile)) {
										// 找到目标文件或目录
										fsId := dirFile.Get("fs_id").Int()
										isDir := dirFile.Get("isdir").Int() == 1
										
										if isDir {
											fmt.Printf("在父目录中找到目标目录: %s, FSID: %d\n", fileName, fsId)
										} else {
											fmt.Printf("在父目录中找到目标文件: %s, FSID: %d\n", fileName, fsId)
										}
										
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
							}
						}
					}
				}
			}
		}
	}
	
	// 如果找不到父目录或父目录中没有目标文件，尝试其他方法
	if !parentDirFound {
		fmt.Println("未找到父目录或父目录中没有目标文件，尝试分析路径结构...")
		
		// 获取路径中最后两级目录
		targetDirPath := ""
		if len(pathParts) > 1 {
			lastDirParts := pathParts[len(pathParts)-2:]
			if len(lastDirParts) == 2 {
				// 使用最后两级目录 (例如 202504/20250407.7z)
				targetDirPath = "/" + lastDirParts[0]
			}
		}
		
		if targetDirPath != "" {
			fmt.Printf("尝试访问目标路径的上级目录: %s\n", targetDirPath)
			
			// 在根目录列表中查找目标目录
			for _, file := range filesData {
				filePath := file.Get("path").String()
				fileName := file.Get("server_filename").String()
				isDir := file.Get("isdir").Int() == 1
				
				if isDir && (fileName == pathParts[len(pathParts)-2] || 
				             strings.Contains(decodeUnicode(filePath), decodeUnicode(targetDirPath))) {
					fmt.Printf("找到匹配的目录: %s, 路径: %s\n", fileName, filePath)
					
					// 如果发现目录名与目标文件名完全匹配，直接使用该目录
					if fileName == targetFile {
						// 找到了完全匹配的目录，直接使用它
						fsId := file.Get("fs_id").Int()
						fmt.Printf("找到完全匹配的目标目录: %s, FSID: %d\n", fileName, fsId)
						
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
							dirFiles := gjson.Get(string(dirData), `list`).Array()
							fmt.Printf("目录中有 %d 个文件/目录\n", len(dirFiles))
							
							// 先检查目录中是否存在与目标文件名完全匹配的目录
							for _, dirFile := range dirFiles {
								fileName := dirFile.Get("server_filename").String()
								isDir := dirFile.Get("isdir").Int() == 1
								
								if isDir && fileName == targetFile {
									// 找到了目标目录，直接使用此目录的fs_id
									fsId := dirFile.Get("fs_id").Int()
									fmt.Printf("找到目标目录: %s, FSID: %d\n", fileName, fsId)
									
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
							
							// 在目录中查找目标文件
							for _, dirFile := range dirFiles {
								fileName := dirFile.Get("server_filename").String()
								decodedFileName := decodeUnicode(fileName)
								decodedTargetFile := decodeUnicode(targetFile)
								isDir := dirFile.Get("isdir").Int() == 1
								
								// 修复三元运算符语法
								dirType := ""
								if isDir {
									dirType = ", 类型: 目录"
								}
								fmt.Printf("  检查文件: %s (%s)%s\n", fileName, decodedFileName, dirType)
								
								if fileName == targetFile || 
								   decodedFileName == decodedTargetFile || 
								   strings.Contains(fileName, targetFile) ||
								   strings.Contains(decodedFileName, decodedTargetFile) {
									// 找到目标文件/目录
									fsId := dirFile.Get("fs_id").Int()
									if isDir {
										fmt.Printf("在深度搜索中找到目标目录: %s, FSID: %d\n", fileName, fsId)
									} else {
										fmt.Printf("在深度搜索中找到目标文件: %s, FSID: %d\n", fileName, fsId)
									}
									
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
						}
					}
				}
			}
		}
	}
	
	// 如果所有方法都失败，尝试深度优先搜索所有目录
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
					dirFiles := gjson.Get(string(dirData), `list`).Array()
					fmt.Printf("  目录中有 %d 个文件/目录\n", len(dirFiles))
					
					// 在目录中查找目标文件
					for _, dirFile := range dirFiles {
						curFileName := dirFile.Get("server_filename").String()
						decodedFileName := decodeUnicode(curFileName)
						decodedTargetFile := decodeUnicode(targetFile)
						
						// 修复三元运算符语法
						dirType := ""
						if isDir {
							dirType = ", 类型: 目录"
						}
						fmt.Printf("  检查文件: %s (%s)%s\n", curFileName, decodedFileName, dirType)
						
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
