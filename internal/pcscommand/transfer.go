package pcscommand

import (
	"encoding/base64"
	"fmt"
	"github.com/qjfoidnh/BaiduPCS-Go/baidupcs"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"net/url"
	"github.com/tidwall/gjson"
	"io/ioutil"
	"net/http"
)

// RunShareTransfer 执行分享链接转存到网盘
func RunShareTransfer(params []string, opt *baidupcs.TransferOption) {
	var link string
	var extraCode string
	var targetPath string
	
	// 检查是否包含目标路径
	if len(params) >= 1 && strings.HasPrefix(params[0], "/") {
		// 第一个参数是目标路径
		targetPath = params[0]
		params = params[1:] // 移除第一个参数
	}
	
	if len(params) == 1 {
		link = params[0]
		if strings.Contains(link, "bdlink=") || !strings.Contains(link, "pan.baidu.com/") {
			//RunRapidTransfer(link, opt.Rname)
			fmt.Printf("%s失败: %s\n", baidupcs.OperationShareFileSavetoLocal, "秒传已不再被支持")
			return
		}
		extraCode = "none"
		if strings.Contains(link, "?pwd=") {
			extraCode = strings.Split(link, "?pwd=")[1]
			link = strings.Split(link, "?pwd=")[0]
		}
	} else if len(params) == 2 {
		link = params[0]
		extraCode = params[1]
	} else if len(params) == 0 {
		fmt.Printf("%s失败: %s\n", baidupcs.OperationShareFileSavetoLocal, "请提供分享链接")
		return
	}
	
	if link[len(link)-1:] == "/" {
		link = link[0 : len(link)-1]
	}
	featureStrs := strings.Split(link, "/")
	featureStr := featureStrs[len(featureStrs)-1]
	if strings.Contains(featureStr, "init?") {
		featureStr = "1" + strings.Split(featureStr, "=")[1]
	}
	if len(featureStr) > 23 || featureStr[0:1] != "1" || len(extraCode) != 4 {
		fmt.Printf("%s失败: %s\n", baidupcs.OperationShareFileSavetoLocal, "链接地址或提取码非法")
		return
	}
	pcs := GetBaiduPCS()
	tokens := pcs.AccessSharePage(featureStr, true)
	if tokens["ErrMsg"] != "0" {
		fmt.Printf("%s失败: %s\n", baidupcs.OperationShareFileSavetoLocal, tokens["ErrMsg"])
		return
	}

	if extraCode != "none" {
		verifyUrl := pcs.GenerateShareQueryURL("verify", map[string]string{
			"shareid":    tokens["shareid"],
			"time":       strconv.Itoa(int(time.Now().UnixMilli())),
			"clienttype": "1",
			"uk":         tokens["share_uk"],
		}).String()
		res := pcs.PostShareQuery(verifyUrl, link, map[string]string{
			"pwd":       extraCode,
			"vcode":     "null",
			"vcode_str": "null",
			"bdstoken":  tokens["bdstoken"],
		})
		if res["ErrMsg"] != "0" {
			fmt.Printf("%s失败: %s\n", baidupcs.OperationShareFileSavetoLocal, res["ErrMsg"])
			return
		}
	}
	pcs.UpdatePCSCookies(true)

	tokens = pcs.AccessSharePage(featureStr, false)
	if tokens["ErrMsg"] != "0" {
		fmt.Printf("%s失败: %s\n", baidupcs.OperationShareFileSavetoLocal, tokens["ErrMsg"])
		return
	}
	
	var transMetas map[string]string
	
	// 根据不同情况获取需要转存的文件列表
	if targetPath != "" {
		// 如果指定了目标路径，自动查找该路径对应的文件
		fmt.Printf("正在查找路径: %s\n", targetPath)
		fmt.Println("解析中，请等待...")
		
		// 首先尝试直接获取指定路径的最后一部分
		pathParts := strings.Split(strings.Trim(targetPath, "/"), "/")
		targetFileName := ""
		if len(pathParts) > 0 {
			targetFileName = pathParts[len(pathParts)-1]
		}
		
		// 先获取根目录文件列表，如果能直接从中找到目标文件就更高效
		featureMap := map[string]string{
			"bdstoken": tokens["bdstoken"],
			"root":     "1",
			"web":      "5",
			"app_id":   baidupcs.PanAppID,
			"shorturl": featureStr[1:],
			"channel":  "chunlei",
		}
		queryShareInfoUrl := pcs.GenerateShareQueryURL("list", featureMap).String()
		
		fmt.Println("获取根目录文件列表...")
		rootDataReadCloser, panError := pcs.SendReqReturnReadCloser(baidupcs.ReqTypePan, baidupcs.OperationShareFileSavetoLocal, http.MethodGet, queryShareInfoUrl, nil, map[string]string{
			"User-Agent":   "Mozilla/5.0",
			"Content-Type": "application/x-www-form-urlencoded; charset=UTF-8",
		})
		
		if panError == nil {
			rootBody, _ := ioutil.ReadAll(rootDataReadCloser)
			rootDataReadCloser.Close()
			
			rootErrno := gjson.Get(string(rootBody), `errno`).Int()
			if rootErrno == 0 {
				// 成功获取根目录列表
				rootFiles := gjson.Get(string(rootBody), `list`).Array()
				fmt.Printf("根目录中找到 %d 个文件/目录\n", len(rootFiles))
				
				// 检查根目录中的文件列表
				var matchedFile gjson.Result
				var foundMatch bool
				
				for _, file := range rootFiles {
					filePath := file.Get("path").String()
					fileName := file.Get("server_filename").String()
					isDir := file.Get("isdir").Int() == 1
					
					fmt.Printf("  - 条目: %s (路径: %s, 是目录: %v)\n", fileName, filePath, isDir)
					
					// 检查此文件是否是我们要找的文件
					if fileName == targetFileName {
						matchedFile = file
						foundMatch = true
						fmt.Printf("在根目录中找到目标文件名匹配: %s\n", fileName)
						break
					}
					
					// 检查路径是否包含目标路径
					if strings.Contains(filePath, targetFileName) {
						matchedFile = file
						foundMatch = true
						fmt.Printf("在根目录中找到路径包含目标文件名: %s 包含 %s\n", filePath, targetFileName)
						break
					}
					
					// 如果是目录，且目标路径包含多级目录，检查是否是目标路径的一部分
					if isDir && len(pathParts) > 1 {
						// 检查此目录是否是目标路径的开始部分
						dirPath := strings.Split(strings.TrimPrefix(filePath, "/"), "/")
						if len(dirPath) > 0 && dirPath[0] == pathParts[0] {
							matchedFile = file
							foundMatch = true
							fmt.Printf("找到目标路径的起始目录: %s\n", fileName)
							break
						}
					}
				}
				
				// 如果在根目录找到了匹配
				if foundMatch {
					fsId := matchedFile.Get("fs_id").Int()
					fileName := matchedFile.Get("server_filename").String()
					isDir := matchedFile.Get("isdir").Int() == 1
					filePath := matchedFile.Get("path").String()
					
					if !isDir && fileName == targetFileName {
						// 如果是文件且名称完全匹配，直接转存
						fmt.Printf("直接找到目标文件: %s (ID: %d)\n", fileName, fsId)
						
						transMetas = make(map[string]string)
						transMetas["shareid"] = tokens["shareid"]
						transMetas["from"] = tokens["share_uk"]
						transMetas["bdstoken"] = tokens["bdstoken"]
						transMetas["filename"] = fileName
						transMetas["item_num"] = "1"
						transMetas["ErrMsg"] = "success"
						transMetas["fs_id"] = fmt.Sprintf("[%d]", fsId)
						
						// 构建转存用URL
						shareUrl := &url.URL{
							Scheme: baidupcs.GetHTTPScheme(true),
							Host:   baidupcs.PanBaiduCom,
							Path:   "/share/transfer",
						}
						uv := shareUrl.Query()
						uv.Set("app_id", baidupcs.PanAppID)
						uv.Set("channel", "chunlei")
						uv.Set("clienttype", "0")
						uv.Set("web", "1")
						for key, value := range transMetas {
							uv.Set(key, value)
						}
						shareUrl.RawQuery = uv.Encode()
						transMetas["shareUrl"] = shareUrl.String()
					} else if isDir {
						// 如果是目录，看看这个目录是否包含我们要找的文件
						fmt.Printf("找到目标目录: %s，查询其内容\n", fileName)
						
						// 尝试查询此目录的内容
						dirList := pcs.GetShareDirList(featureStr, filePath, tokens["shareid"], tokens["share_uk"], tokens["bdstoken"])
						
						if dirList["ErrMsg"] == "success" {
							fmt.Printf("成功获取目录 %s 的内容\n", fileName)
							
							// 检查是否是路径最后一部分
							if fileName == targetFileName {
								// 直接使用此目录的内容
								transMetas = dirList
							} else {
								// 尝试在当前目录中查找目标文件
								dirContentUrl := pcs.GenerateShareQueryURL("list", map[string]string{
									"bdstoken": tokens["bdstoken"],
									"root":     "0",
									"web":      "5",
									"app_id":   baidupcs.PanAppID,
									"shorturl": featureStr[1:],
									"channel":  "chunlei",
									"dir":      filePath,
								}).String()
								
								dirContentDataReadCloser, dirContentPanError := pcs.SendReqReturnReadCloser(baidupcs.ReqTypePan, baidupcs.OperationShareFileSavetoLocal, http.MethodGet, dirContentUrl, nil, map[string]string{
									"User-Agent":   "Mozilla/5.0",
									"Content-Type": "application/x-www-form-urlencoded; charset=UTF-8",
								})
								
								if dirContentPanError == nil {
									dirContentBody, _ := ioutil.ReadAll(dirContentDataReadCloser)
									dirContentDataReadCloser.Close()
									
									dirContentErrno := gjson.Get(string(dirContentBody), `errno`).Int()
									if dirContentErrno == 0 {
										dirContentFiles := gjson.Get(string(dirContentBody), `list`).Array()
										fmt.Printf("目录 %s 中找到 %d 个文件/目录\n", fileName, len(dirContentFiles))
										
										// 查找目标文件
										for _, dirFile := range dirContentFiles {
											dirFileName := dirFile.Get("server_filename").String()
											dirFilePath := dirFile.Get("path").String()
											dirFileIsDir := dirFile.Get("isdir").Int() == 1
											
											fmt.Printf("  检查: %s (路径: %s, 是目录: %v)\n", dirFileName, dirFilePath, dirFileIsDir)
											
											// 看看是否匹配目标文件名
											if dirFileName == targetFileName {
												foundFileId := dirFile.Get("fs_id").Int()
												
												// 创建转存信息
												transMetas = make(map[string]string)
												transMetas["shareid"] = tokens["shareid"]
												transMetas["from"] = tokens["share_uk"]
												transMetas["bdstoken"] = tokens["bdstoken"]
												transMetas["filename"] = dirFileName
												transMetas["item_num"] = "1"
												transMetas["ErrMsg"] = "success"
												transMetas["fs_id"] = fmt.Sprintf("[%d]", foundFileId)
												
												// 构建转存用URL
												shareUrl := &url.URL{
													Scheme: baidupcs.GetHTTPScheme(true),
													Host:   baidupcs.PanBaiduCom,
													Path:   "/share/transfer",
												}
												uv := shareUrl.Query()
												uv.Set("app_id", baidupcs.PanAppID)
												uv.Set("channel", "chunlei")
												uv.Set("clienttype", "0")
												uv.Set("web", "1")
												for key, value := range transMetas {
													uv.Set(key, value)
												}
												shareUrl.RawQuery = uv.Encode()
												transMetas["shareUrl"] = shareUrl.String()
												
												fmt.Printf("在目录 %s 中找到目标文件: %s\n", fileName, dirFileName)
												break
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
		
		// 如果无法从根目录直接找到，尝试完整路径查找
		if transMetas == nil {
			// 检查是否是JSON格式的目标路径
			if strings.HasPrefix(targetPath, "{") && strings.HasSuffix(targetPath, "}") {
				fmt.Println("检测到JSON格式的目标路径，尝试直接解析...")
				
				// 尝试解析JSON
				fileList := gjson.Get(targetPath, "list").Array()
				fmt.Printf("JSON中包含 %d 个文件/目录\n", len(fileList))
				
				// 查找我们想要的文件
				var targetFsId int64 = 0
				var targetFilename string
				
				// 提取完整路径（去掉JSON部分）
				fullPath := ""
				// targetPath可能是一个完整的JSON响应，也可能是在路径后面附加了JSON
				jsonStart := strings.Index(targetPath, "{")
				if jsonStart > 0 {
					fullPath = targetPath[:jsonStart]
				}
				
				// 解析路径并寻找最深层的文件/目录
				if fullPath != "" {
					pathParts := strings.Split(strings.Trim(fullPath, "/"), "/")
					if len(pathParts) > 0 {
						targetFilename = pathParts[len(pathParts)-1]
						fmt.Printf("从路径提取文件名: %s\n", targetFilename)
						
						// 在文件列表中查找该文件
						for _, file := range fileList {
							fileName := file.Get("server_filename").String()
							if fileName == targetFilename {
								targetFsId = file.Get("fs_id").Int()
								fmt.Printf("找到匹配文件: %s, FSID: %d\n", fileName, targetFsId)
								break
							}
						}
					}
				}
				
				// 如果没有找到指定文件，但有文件列表，选择第一个
				if targetFsId == 0 && len(fileList) > 0 {
					targetFsId = fileList[0].Get("fs_id").Int()
					targetFilename = fileList[0].Get("server_filename").String()
					fmt.Printf("使用列表中的第一个文件: %s, FSID: %d\n", targetFilename, targetFsId)
				}
				
				if targetFsId > 0 {
					transMetas = make(map[string]string)
					transMetas["shareid"] = tokens["shareid"]
					transMetas["from"] = tokens["share_uk"]
					transMetas["bdstoken"] = tokens["bdstoken"]
					transMetas["filename"] = targetFilename
					transMetas["item_num"] = "1"
					transMetas["ErrMsg"] = "success"
					transMetas["fs_id"] = fmt.Sprintf("[%d]", targetFsId)
					
					// 构建转存用URL
					shareUrl := &url.URL{
						Scheme: baidupcs.GetHTTPScheme(true),
						Host:   baidupcs.PanBaiduCom,
						Path:   "/share/transfer",
					}
					uv := shareUrl.Query()
					uv.Set("app_id", baidupcs.PanAppID)
					uv.Set("channel", "chunlei")
					uv.Set("clienttype", "0")
					uv.Set("web", "1")
					for key, value := range transMetas {
						uv.Set(key, value)
					}
					shareUrl.RawQuery = uv.Encode()
					transMetas["shareUrl"] = shareUrl.String()
				} else {
					// 如果还是找不到，则使用常规方法
					fmt.Println("无法从JSON中找到目标文件，尝试常规路径查找...")
					transMetas = pcs.GetShareFileByPath(featureStr, targetPath, tokens["shareid"], tokens["share_uk"], tokens["bdstoken"])
				}
			} else {
				// 常规路径查找
				transMetas = pcs.GetShareFileByPath(featureStr, targetPath, tokens["shareid"], tokens["share_uk"], tokens["bdstoken"])
			}
		}
	} else if opt.FsId > 0 {
		// 如果指定了特定文件ID，则尝试转存该文件
		fmt.Printf("正在获取文件ID: %d 的信息\n", opt.FsId)
		transMetas = pcs.GetShareFileByFsId(featureStr, opt.Dir, tokens["shareid"], tokens["share_uk"], tokens["bdstoken"], opt.FsId)
	} else if opt.Dir != "" {
		// 如果指定了目录但没有文件ID，则获取该目录下所有文件
		fmt.Printf("正在获取目录: %s 的文件列表\n", opt.Dir)
		transMetas = pcs.GetShareDirList(featureStr, opt.Dir, tokens["shareid"], tokens["share_uk"], tokens["bdstoken"])
	} else {
		// 默认获取根目录的文件列表
		fmt.Println("获取根目录文件列表...")
		featureMap := map[string]string{
			"bdstoken": tokens["bdstoken"],
			"root":     "1",
			"web":      "5",
			"app_id":   baidupcs.PanAppID,
			"shorturl": featureStr[1:],
			"channel":  "chunlei",
		}
		queryShareInfoUrl := pcs.GenerateShareQueryURL("list", featureMap).String()
		transMetas = pcs.ExtractShareInfo(queryShareInfoUrl, tokens["shareid"], tokens["share_uk"], tokens["bdstoken"])
	}

	if transMetas["ErrMsg"] != "success" {
		fmt.Printf("%s失败: %s\n", baidupcs.OperationShareFileSavetoLocal, transMetas["ErrMsg"])
		return
	}
	
	transMetas["path"] = GetActiveUser().Workdir
	if transMetas["item_num"] != "1" && opt.Collect {
		transMetas["filename"] += "等文件"
		transMetas["path"] = path.Join(GetActiveUser().Workdir, transMetas["filename"])
		pcs.Mkdir(transMetas["path"])
	}
	transMetas["referer"] = "https://pan.baidu.com/s/" + featureStr
	pcs.UpdatePCSCookies(true)
	
	// 显示转存信息
	if targetPath != "" || opt.FsId > 0 || opt.Dir != "" {
		fmt.Printf("准备转存: %s 到当前目录\n", transMetas["filename"])
	}
	
	resp := pcs.GenerateRequestQuery("POST", transMetas)
	if resp["ErrNo"] != "0" {
		fmt.Printf("%s失败: %s\n", baidupcs.OperationShareFileSavetoLocal, resp["ErrMsg"])
		//if resp["ErrNo"] == "4" {
		//	transMetas["shorturl"] = featureStr
		//	pcs.SuperTransfer(transMetas, resp["limit"]) // 试验性功能, 当前未启用
		//}
		return
	}
	if opt.Collect {
		resp["filename"] = transMetas["filename"]
	}
	fmt.Printf("%s成功, 保存了%s到当前目录\n", baidupcs.OperationShareFileSavetoLocal, resp["filename"])
	if opt.Download {
		fmt.Println("即将开始下载")
		paths := strings.Split(resp["filenames"], ",")
		paths = paths[0 : len(paths)-1]
		RunDownload(paths, nil)
	}
}

// RunRapidTransfer 执行秒传链接解析及保存
func RunRapidTransfer(link string, rnameOpt ...bool) {
	if strings.Contains(link, "bdlink=") || strings.Contains(link, "bdpan://") {
		r, _ := regexp.Compile(`(bdlink=|bdpan://)([^\s]+)`)
		link1 := r.FindStringSubmatch(link)[2]
		decodeBytes, err := base64.StdEncoding.DecodeString(link1)
		if err != nil {
			fmt.Printf("%s失败: %s\n", baidupcs.OperationRapidLinkSavetoLocal, "秒传链接格式错误")
			return
		}
		link = string(decodeBytes)
	}
	rname := false
	if len(rnameOpt) > 0 {
		rname = rnameOpt[0]
	}
	link = strings.TrimSpace(link)
	substrs := strings.SplitN(link, "#", 4)
	if len(substrs) == 4 {
		md5, slicemd5 := substrs[0], substrs[1]
		size, _ := strconv.ParseInt(substrs[2], 10, 64)
		filename := path.Join(GetActiveUser().Workdir, randReplaceStr(substrs[3], rname))
		RunRapidUpload(filename, md5, slicemd5, size)
	} else if len(substrs) == 3 {
		md5 := substrs[0]
		size, _ := strconv.ParseInt(substrs[1], 10, 64)
		filename := path.Join(GetActiveUser().Workdir, randReplaceStr(substrs[2], rname))
		RunRapidUpload(filename, md5, "", size)
	} else {
		fmt.Printf("%s失败: %s\n", baidupcs.OperationRapidLinkSavetoLocal, "秒传链接格式错误")
	}
	return
}
