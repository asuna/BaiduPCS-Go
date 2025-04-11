package pcscommand

import (
	"encoding/base64"
	"fmt"
	"github.com/qjfoidnh/BaiduPCS-Go/baidupcs"
	"github.com/qjfoidnh/BaiduPCS-Go/requester"
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
	
	// 处理select选项 - 选择性转存部分文件或目录
	if opt.Select != "" && transMetas["item_num"] != "1" {
		fmt.Println("使用选择性转存功能，筛选文件/目录...")
		
		// 提取待选择的文件/目录名称列表，同时处理可能包含完整路径的情况
		selectedItems := strings.Split(opt.Select, ",")
		fmt.Printf("需要转存的项目: %s\n", opt.Select)
		
		// 将选择的文件按目录进行分组
		selectedFilesByDir := make(map[string][]string)
		
		// 标记是否有直接选择目录的情况
		hasDirectorySelection := false
		var directoryPaths []string
		
		// 处理每个选择项，按目录分组
		for _, item := range selectedItems {
			item = strings.TrimSpace(item)
			
			// 检查是否是直接选择的目录路径（没有文件名部分）
			isDirectoryPath := false
			
			// 如果是完整路径，提取目录和文件名
			if strings.Contains(item, "/") {
				pathParts := strings.Split(strings.Trim(item, "/"), "/")
				
				// 检查网盘API返回的根目录中是否有这个目录
				// 获取根目录列表
				featureMap := map[string]string{
					"bdstoken": tokens["bdstoken"],
					"root":     "1",
					"web":      "5",
					"app_id":   baidupcs.PanAppID,
					"shorturl": featureStr[1:],
					"channel":  "chunlei",
				}
				queryRootUrl := pcs.GenerateShareQueryURL("list", featureMap).String()
				
				rootDataReadCloser, panError := pcs.SendReqReturnReadCloser(baidupcs.ReqTypePan, baidupcs.OperationShareFileSavetoLocal, http.MethodGet, queryRootUrl, nil, map[string]string{
					"User-Agent":   requester.UserAgent,
					"Content-Type": "application/x-www-form-urlencoded; charset=UTF-8",
				})
				
				var rootFiles []gjson.Result
				if panError == nil {
					rootBody, _ := ioutil.ReadAll(rootDataReadCloser)
					rootDataReadCloser.Close()
					
					rootErrno := gjson.Get(string(rootBody), `errno`).Int()
					if rootErrno == 0 {
						rootFiles = gjson.Get(string(rootBody), `list`).Array()
					}
				}
				
				// 检查是否在根目录中找到匹配的目录
				dirNameToMatch := pathParts[len(pathParts)-1]
				for _, rootFile := range rootFiles {
					rootFileName := rootFile.Get("server_filename").String()
					rootFilePath := rootFile.Get("path").String()
					rootFileIsDir := rootFile.Get("isdir").Int() == 1
					
					if rootFileIsDir && (rootFileName == dirNameToMatch || 
						strings.Contains(rootFilePath, item)) {
						// 在根目录中找到匹配的目录
						isDirectoryPath = true
						hasDirectorySelection = true
						directoryPaths = append(directoryPaths, item)
						fmt.Printf("检测到直接选择的目录: %s\n", item)
						break
					}
				}
				
				// 如果不是直接选择的目录，那么按常规处理
				if !isDirectoryPath {
					lastSlashIndex := strings.LastIndex(item, "/")
					if lastSlashIndex > 0 {
						dirPath := item[:lastSlashIndex]
						fileName := item[lastSlashIndex+1:]
						
						// 确保目录路径格式一致
						if !strings.HasPrefix(dirPath, "/") {
							dirPath = "/" + dirPath
						}
						
						// 添加到对应目录的文件列表
						if _, ok := selectedFilesByDir[dirPath]; !ok {
							selectedFilesByDir[dirPath] = []string{}
						}
						selectedFilesByDir[dirPath] = append(selectedFilesByDir[dirPath], fileName)
						fmt.Printf("目录 %s 的文件: %s\n", dirPath, fileName)
					} else {
						// 处理根目录下的文件
						fileName := item[1:] // 去掉前导斜杠
						if _, ok := selectedFilesByDir["/"];!ok {
							selectedFilesByDir["/"] = []string{}
						}
						selectedFilesByDir["/"] = append(selectedFilesByDir["/"], fileName)
						fmt.Printf("根目录的文件: %s\n", fileName)
					}
				}
			} else {
				// 无路径的情况，默认为根目录
				if _, ok := selectedFilesByDir["/"];!ok {
					selectedFilesByDir["/"] = []string{}
				}
				selectedFilesByDir["/"] = append(selectedFilesByDir["/"], item)
				fmt.Printf("根目录的文件: %s\n", item)
			}
		}

		// 用于收集所有匹配的文件
		var allMatchedFsIds []int64
		var allMatchedNames []string
		
		// 如果有直接选择的目录，优先处理这些目录
		if hasDirectorySelection {
			for _, dirPath := range directoryPaths {
				fmt.Printf("处理直接选择的目录: %s\n", dirPath)
				
				// 使用GetShareFileByPath获取目录信息
				dirInfo := pcs.GetShareFileByPath(featureStr, dirPath, tokens["shareid"], tokens["share_uk"], tokens["bdstoken"])
				
				if dirInfo["ErrMsg"] == "success" {
					// 成功获取目录信息 
					// 检查是否找到的是目录本身的fs_id，而不是目录内容的fs_id
					// 在根目录中尝试查找匹配的目录ID
					pathParts := strings.Split(strings.Trim(dirPath, "/"), "/")
					if len(pathParts) > 0 {
						dirName := pathParts[len(pathParts)-1]
						fmt.Printf("查找目录: %s 的实际fs_id\n", dirName)
						
						// 查询根目录内容
						rootUrl := pcs.GenerateShareQueryURL("list", map[string]string{
							"bdstoken": tokens["bdstoken"],
							"root":     "1",
							"web":      "5",
							"app_id":   baidupcs.PanAppID,
							"shorturl": featureStr[1:],
							"channel":  "chunlei",
						}).String()
						
						rootDataCloser, _ := pcs.SendReqReturnReadCloser(baidupcs.ReqTypePan, baidupcs.OperationShareFileSavetoLocal, http.MethodGet, rootUrl, nil, map[string]string{
							"User-Agent":   requester.UserAgent,
							"Content-Type": "application/x-www-form-urlencoded; charset=UTF-8",
						})
						
						if rootDataCloser != nil {
							rootData, _ := ioutil.ReadAll(rootDataCloser)
							rootDataCloser.Close()
							
							rootErrno := gjson.Get(string(rootData), `errno`).Int()
							if rootErrno == 0 {
								rootFiles := gjson.Get(string(rootData), `list`).Array()
								
								// 查找对应目录的ID
								for _, file := range rootFiles {
									fileName := file.Get("server_filename").String()
									filePath := file.Get("path").String()
									
									if fileName == dirName || strings.HasSuffix(filePath, dirPath) {
										dirFsId := file.Get("fs_id").Int()
										isDir := file.Get("isdir").Int() == 1
										if isDir {
											fmt.Printf("找到目录 %s 的fs_id: %d\n", dirName, dirFsId)
											allMatchedFsIds = append(allMatchedFsIds, dirFsId)
											allMatchedNames = append(allMatchedNames, fileName)
											break
										}
									}
								}
							}
						}
						
						// 如果找不到目录本身，尝试使用fs_id字段的第一个值
						if len(allMatchedFsIds) == 0 {
							fsIdStr := dirInfo["fs_id"]
							// 尝试提取目录ID而不是内容ID
							fmt.Printf("尝试从接口返回获取目录ID: %s\n", fsIdStr)
							
							// 移除方括号
							fsIdStr = strings.Trim(fsIdStr, "[]")
							if fsIdStr != "" {
								fsIdParts := strings.Split(fsIdStr, ",")
								// 只取第一个ID，这通常是目录本身
								if len(fsIdParts) > 0 {
									firstId, err := strconv.ParseInt(fsIdParts[0], 10, 64)
									if err == nil {
										fmt.Printf("使用第一个ID作为目录ID: %d\n", firstId)
										allMatchedFsIds = append(allMatchedFsIds, firstId)
										// 使用路径最后部分作为目录名称
										dirName := pathParts[len(pathParts)-1]
										allMatchedNames = append(allMatchedNames, dirName)
									}
								}
							}
						}
					}
				} else {
					fmt.Printf("获取目录信息失败: %s\n", dirInfo["ErrMsg"])
					
					// 如果通过接口获取失败，尝试从共享文件列表中直接查找
					fmt.Println("尝试从共享根目录获取目录信息...")
					
					// 获取共享根目录
					rootUrl := pcs.GenerateShareQueryURL("list", map[string]string{
						"bdstoken": tokens["bdstoken"],
						"root":     "1",
						"web":      "5",
						"app_id":   baidupcs.PanAppID,
						"shorturl": featureStr[1:],
						"channel":  "chunlei",
					}).String()
					
					rootDataCloser, _ := pcs.SendReqReturnReadCloser(baidupcs.ReqTypePan, baidupcs.OperationShareFileSavetoLocal, http.MethodGet, rootUrl, nil, map[string]string{
						"User-Agent":   requester.UserAgent,
						"Content-Type": "application/x-www-form-urlencoded; charset=UTF-8",
					})
					
					if rootDataCloser != nil {
						rootData, _ := ioutil.ReadAll(rootDataCloser)
						rootDataCloser.Close()
						
						rootErrno := gjson.Get(string(rootData), `errno`).Int()
						if rootErrno == 0 {
							rootFiles := gjson.Get(string(rootData), `list`).Array()
							fmt.Printf("共享根目录中有 %d 个文件/目录\n", len(rootFiles))
							
							// 尝试匹配目录路径
							pathParts := strings.Split(strings.Trim(dirPath, "/"), "/")
							if len(pathParts) > 0 {
								dirName := pathParts[len(pathParts)-1]
								
								for _, file := range rootFiles {
									fileName := file.Get("server_filename").String()
									filePath := file.Get("path").String()
									isDir := file.Get("isdir").Int() == 1
									
									if isDir && (fileName == dirName || strings.Contains(filePath, dirPath)) {
										fsId := file.Get("fs_id").Int()
										fmt.Printf("在共享根目录中找到目录: %s, fs_id: %d\n", fileName, fsId)
										allMatchedFsIds = append(allMatchedFsIds, fsId)
										allMatchedNames = append(allMatchedNames, fileName)
										break
									}
								}
							}
						}
					}
				}
			}
		}
		
		// 匹配函数，检查文件名是否匹配选择列表
		matchFile := func(filename string, isDir bool, fileList []string) bool {
			for _, item := range fileList {
				item = strings.TrimSpace(item)
				
				if item == filename {
					return true
				}
				// 支持通配符 *
				if strings.Contains(item, "*") {
					pattern := strings.Replace(item, "*", ".*", -1)
					match, _ := regexp.MatchString("^"+pattern+"$", filename)
					if match {
						return true
					}
				}
			}
			return false
		}

		// 为每个目录查询文件列表并匹配文件
		for dirPath, fileList := range selectedFilesByDir {
			fmt.Printf("在目录 %s 中查找 %d 个文件\n", dirPath, len(fileList))
			
			// 构建目录查询URL
			var dirUrl string
			
			if dirPath == "/" {
				// 查询根目录
				dirUrl = pcs.GenerateShareQueryURL("list", map[string]string{
					"bdstoken": tokens["bdstoken"],
					"root":     "1",
					"web":      "5",
					"app_id":   baidupcs.PanAppID,
					"shorturl": featureStr[1:],
					"channel":  "chunlei",
				}).String()
				fmt.Println("查询根目录内容")
			} else {
				// 查询指定目录
				dirUrl = pcs.GenerateShareQueryURL("list", map[string]string{
					"bdstoken": tokens["bdstoken"],
					"root":     "0",
					"web":      "5",
					"app_id":   baidupcs.PanAppID,
					"shorturl": featureStr[1:],
					"channel":  "chunlei",
					"dir":      dirPath,
				}).String()
				fmt.Printf("查询目录内容: %s\n", dirUrl)
			}
			
			// 发送请求获取目录内容
			dirDataCloser, dirError := pcs.SendReqReturnReadCloser(baidupcs.ReqTypePan, baidupcs.OperationShareFileSavetoLocal, http.MethodGet, dirUrl, nil, map[string]string{
				"User-Agent":   requester.UserAgent,
				"Content-Type": "application/x-www-form-urlencoded; charset=UTF-8",
			})
			
			if dirError != nil {
				fmt.Printf("获取目录 %s 内容失败: %s\n", dirPath, dirError.Error())
				continue
			}
			
			dirData, _ := ioutil.ReadAll(dirDataCloser)
			dirDataCloser.Close()
			
			dirErrno := gjson.Get(string(dirData), `errno`).Int()
			if dirErrno != 0 {
				fmt.Printf("获取目录 %s 内容失败，错误码: %d\n", dirPath, dirErrno)
				
				// 尝试使用GetShareFileByPath获取目录信息
				fmt.Printf("尝试通过GetShareFileByPath获取目录信息: %s\n", dirPath)
				dirInfo := pcs.GetShareFileByPath(featureStr, dirPath, tokens["shareid"], tokens["share_uk"], tokens["bdstoken"])
				
				if dirInfo["ErrMsg"] != "success" {
					fmt.Printf("获取目录 %s 信息失败: %s\n", dirPath, dirInfo["ErrMsg"])
					continue
				}
				
				// 尝试重新获取目录内容
				dirUrl = pcs.GenerateShareQueryURL("list", map[string]string{
					"bdstoken": tokens["bdstoken"],
					"root":     "0",
					"web":      "5",
					"app_id":   baidupcs.PanAppID,
					"shorturl": featureStr[1:],
					"channel":  "chunlei",
					"dir":      dirPath,
				}).String()
				
				dirDataCloser, dirError = pcs.SendReqReturnReadCloser(baidupcs.ReqTypePan, baidupcs.OperationShareFileSavetoLocal, http.MethodGet, dirUrl, nil, map[string]string{
					"User-Agent":   requester.UserAgent,
					"Content-Type": "application/x-www-form-urlencoded; charset=UTF-8",
				})
				
				if dirError != nil {
					fmt.Printf("重新获取目录 %s 内容失败: %s\n", dirPath, dirError.Error())
					continue
				}
				
				dirData, _ = ioutil.ReadAll(dirDataCloser)
				dirDataCloser.Close()
				
				dirErrno = gjson.Get(string(dirData), `errno`).Int()
				if dirErrno != 0 {
					fmt.Printf("重新获取目录 %s 内容失败，错误码: %d\n", dirPath, dirErrno)
					continue
				}
			}
			
			// 获取文件列表
			dirFiles := gjson.Get(string(dirData), `list`).Array()
			fmt.Printf("目录 %s 中有 %d 个文件/目录\n", dirPath, len(dirFiles))
			
			// 在目录中查找匹配的文件
			var dirMatchedCount int = 0
			
			for _, file := range dirFiles {
				filename := file.Get("server_filename").String()
				fsid := file.Get("fs_id").Int()
				isDir := file.Get("isdir").Int() == 1
				
				// 检查是否匹配选择列表
				if matchFile(filename, isDir, fileList) {
					allMatchedFsIds = append(allMatchedFsIds, fsid)
					allMatchedNames = append(allMatchedNames, filename)
					dirMatchedCount++
					fmt.Printf("在目录 %s 中匹配: %s (ID: %d, 是目录: %v)\n", dirPath, filename, fsid, isDir)
				}
			}
			
			fmt.Printf("在目录 %s 中找到 %d 个匹配文件\n", dirPath, dirMatchedCount)
		}
		
		// 检查是否找到匹配的文件
		if len(allMatchedFsIds) == 0 {
			fmt.Println("未找到匹配指定条件的文件或目录")
			return
		}
		
		// 构建新的fs_id列表
		fsIdStr := "["
		for _, id := range allMatchedFsIds {
			fsIdStr += strconv.FormatInt(id, 10) + ","
		}
		fsIdStr = fsIdStr[:len(fsIdStr)-1] + "]" // 去掉最后的逗号，添加右括号
		
		// 更新transMetas信息
		transMetas["fs_id"] = fsIdStr
		transMetas["item_num"] = strconv.Itoa(len(allMatchedFsIds))
		
		if len(allMatchedNames) == 1 {
			transMetas["filename"] = allMatchedNames[0]
		} else {
			transMetas["filename"] = allMatchedNames[0] + "等" + strconv.Itoa(len(allMatchedNames)) + "个文件/目录"
		}
		
		// 重新构建转存URL
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
		
		fmt.Printf("已选择 %d 个文件/目录进行转存\n", len(allMatchedFsIds))
	}
	
	// 处理转存
	transMetas["referer"] = "https://pan.baidu.com/s/" + featureStr
	pcs.UpdatePCSCookies(true)
	
	// 显示转存信息
	if targetPath != "" || opt.FsId > 0 || opt.Dir != "" {
		fmt.Printf("准备转存: %s 到", transMetas["filename"])
		if opt.SaveTo != "" {
			fmt.Printf("目录: %s\n", opt.SaveTo)
		} else {
			fmt.Println("当前目录")
		}
	}
	
	// 如果指定了SaveTo，使用TransferShareFile方法
	if opt.SaveTo != "" {
		result := pcs.TransferShareFile(transMetas, opt.SaveTo)
		if result["ErrMsg"] != "success" {
			fmt.Printf("%s失败: %s\n", baidupcs.OperationShareFileSavetoLocal, result["ErrMsg"])
			return
		}
		fmt.Printf("%s成功, 保存了%s到目录: %s\n", baidupcs.OperationShareFileSavetoLocal, transMetas["filename"], opt.SaveTo)
	} else {
		// 使用原来的方法
		transMetas["path"] = GetActiveUser().Workdir
		if transMetas["item_num"] != "1" && opt.Collect {
			transMetas["filename"] += "等文件"
			transMetas["path"] = path.Join(GetActiveUser().Workdir, transMetas["filename"])
			pcs.Mkdir(transMetas["path"])
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
	}
	
	if opt.Download {
		fmt.Println("即将开始下载")
		paths := strings.Split(transMetas["filenames"], ",")
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
