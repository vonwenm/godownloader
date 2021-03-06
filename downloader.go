package downloader

import (
	"crawler/downloader/graphite"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
	"encoding/base64"
)

const (
	USER_AGENT            = "Mozilla/5.0 (Windows; U; Windows NT 5.1; zh-CN; rv:1.8.1.14) Gecko/20080404 (FoxPlus) Firefox/2.0.0.14"
	DOWNLOADER_QUEUE_SIZE = 512
)

type Downloader interface {
	Download(url string) (string, error)
}

type HTTPGetDownloader struct {
	cleaner *HTMLCleaner
	client  *http.Client
}

func extractSearchQuery(link0 string) string {
	link := link0
	if strings.Contains(link, "realtime?link=") {
		kv := strings.Split(link, "realtime?link=")
		link = kv[1]
		linkbyte, err := base64.URLEncoding.DecodeString(link)
		if err != nil {
			return ""
		}
		link = string(linkbyte)
		log.Println("downloader change link", link0, link)
	}
	params := extractUrlParams(link)
	if strings.Contains(link, "www.baidu.com") {
		ret, ok := params["word"]
		if ok {
			return ret
		}
	} else if strings.Contains(link, "www.sogou.com") {
		ret, ok := params["query"]
		if ok {
			return ret
		}
	} else if strings.Contains(link, "www.so.com") {
		ret, ok := params["q"]
		if ok {
			return ret
		}
	}
	return ""
}

func dialTimeout(network, addr string) (net.Conn, error) {
	timeout := time.Duration(ConfigInstance().DownloadTimeout) * time.Second
	deadline := time.Now().Add(timeout)
	c, err := net.DialTimeout(network, addr, timeout)
	if err != nil {
		return nil, err
	}
	c.SetDeadline(deadline)
	return c, nil
}

func NewHTTPGetDownloader() *HTTPGetDownloader {
	ret := HTTPGetDownloader{}
	ret.cleaner = NewHTMLCleaner()
	ret.client = &http.Client{
		Transport: &http.Transport{
			Dial:                  dialTimeout,
			DisableKeepAlives:     true,
			ResponseHeaderTimeout: time.Duration(ConfigInstance().DownloadTimeout) * time.Second,
		},
	}

	return &ret
}

func NewHTTPGetProxyDownloader(proxy string) *HTTPGetDownloader {
	ret := HTTPGetDownloader{}
	ret.cleaner = NewHTMLCleaner()
	proxyUrl, err := url.Parse(proxy)
	if err != nil {
		return nil
	}
	ret.client = &http.Client{
		Transport: &http.Transport{
			Dial:                  dialTimeout,
			DisableKeepAlives:     true,
			ResponseHeaderTimeout: time.Duration(ConfigInstance().DownloadTimeout) * time.Second,
			Proxy: http.ProxyURL(proxyUrl),
		},
	}
	return &ret
}

func NewDefaultHTTPGetProxyDownloader(proxy string) *HTTPGetDownloader {
	ret := HTTPGetDownloader{}
	ret.cleaner = nil
	proxyUrl, err := url.Parse(proxy)
	if err != nil {
		return nil
	}
	ret.client = &http.Client{
		Transport: &http.Transport{
			DisableKeepAlives: true,
			Proxy:             http.ProxyURL(proxyUrl),
		},
	}
	return &ret
}

func setStatus(query, status string) {
	return
	/*
	client := &http.Client{
		Transport: &http.Transport{
			Dial:                  dialTimeout,
			DisableKeepAlives:     true,
			ResponseHeaderTimeout: time.Duration(ConfigInstance().DownloadTimeout) * time.Second,
		},
	}
	req, err := http.NewRequest("GET", "http://redis.crawler.bdp.cc:8080/LPUSH/search." + query + "/" + strconv.FormatInt(time.Now().Unix(), 10) + "." + status, nil)
	if err != nil{
		return
	}
	resp, err := client.Do(req)
	if err != nil || resp == nil || resp.Body == nil {
		return
	}
	defer resp.Body.Close()
	ioutil.ReadAll(resp.Body)
	*/
}

func (self *HTTPGetDownloader) Download(url string) (string, string, error) {
	query := extractSearchQuery(url)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil || req == nil || req.Header == nil {
		return "", "", err
	}
	
	if strings.Contains(url, "http://www.baidu.com/") {
		expire := time.Now().AddDate(40, 0, 0)
		cookie := http.Cookie{"BAIDUUID", "74D20AADE2DD67FE23016935054F5A24:SL=0:NR=100:FG=1", "/", ".baidu.com", expire, expire.Format(time.UnixDate), 1261440000, true, true, "BAIDUUID=74D20AADE2DD67FE23016935054F5A24:SL=0:NR=100:FG=1", []string{"BAIDUUID=74D20AADE2DD67FE23016935054F5A24:SL=0:NR=100:FG=1"}}
		req.AddCookie(&cookie)
	}
	req.Header.Set("User-Agent", USER_AGENT)
	req.Header.Set("hello", "world")
	resp, err := self.client.Do(req)

	respInfo := ""
	if err != nil || resp == nil || resp.Body == nil {
		return "", "", err
	} else {
		respInfo += "<real_url>" + resp.Request.URL.String() + "</real_url>"
		respInfo += "<content_type>" + resp.Header.Get("Content-Type") + "</content_type>"
		if len(query) > 0 {
			log.Println("extract", query, "from", url)
			respInfo += "<query>" + query + "</query>"
		}
		defer resp.Body.Close()
		if !strings.Contains(resp.Header.Get("Content-Type"), "text/") && !strings.Contains(resp.Header.Get("Content-Type"), "json") {
			return "", "", errors.New("non html page")
		}
		cleanRespInfo := string(self.cleaner.CleanHTML([]byte(respInfo)))
		html, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return "", "", err
		} else {
			if self.cleaner != nil {
				utf8Html := self.cleaner.ToUTF8(html)
				if utf8Html == nil {
					return "", "", errors.New("conver to utf8 error")
				}
				cleanHtml := string(self.cleaner.CleanHTML(utf8Html))
				if IsBlock(cleanHtml) {
					return "", "", errors.New("blocked")
				}
				return string(cleanHtml), cleanRespInfo, nil
			} else {
				return string(html), cleanRespInfo, nil
			}
		}
	}
}

type Link struct {
	LinkURL string `json:"url"`
	Referrer string `json:"referrer"`
}

type PostBody struct {
	Links []Link `json:"links"`
}

type Response struct {
	PostChannelLength      int `json:"post_chan_length"`
	ExtractedChannelLength int `json:"extract_chan_length"`
	CacheSize              int `json:"cache_size"`
}

type WebPage struct {
	Link         string
	Html         string
	RespInfo     string
	DownloadedAt int64
}

type WebSiteStat struct {
	linkRecvCount     map[string]int
	pageDownloadCount map[string]int
	pageWriteCount    map[string]int
}

type DownloadHandler struct {
	ticker *time.Ticker
	metricSender                   *graphite.Client
	LinksChannel                   chan Link
	Downloader                     *HTTPGetDownloader
	ProxyDownloader                []*HTTPGetDownloader
	RtDownloaderAddrs			[]string
	signals                        chan os.Signal
	ExtractedLinksChannel          chan Link
	PageChannel                    chan WebPage
	urlFilter                      *URLFilter
	writer                         *os.File
	currentPath                    string
	flushFileSize                  int
	processedPageCount             int
	totalDownloadedPageCount       int
	proxyDownloadedPageCount       int
	proxyDownloadedPageFailedCount int
	writePageCount                 int
	WebSiteStat
}

func (self *DownloadHandler) WritePage(page WebPage) {
	if !utf8.ValidString(page.Link) {
		return
	}

	if !utf8.ValidString(page.Html) {
		return
	}

	if strings.Contains(page.Link, "realtime?link="){
		kv := strings.Split(page.Link, "realtime?link=")
		page.Link = kv[1]
		link, err := base64.URLEncoding.DecodeString(page.Link)
		if err != nil {
			log.Println("downloader decode base64 error", link, err)
			return
		}
		page.Link = string(link)
	}

	SetBloomFilter(page.Link)

	self.writePageCount += 1

	self.writer.WriteString(strconv.FormatInt(page.DownloadedAt, 10))
	self.writer.WriteString("\t")
	self.writer.WriteString(page.Link)
	self.writer.WriteString("\t")
	self.writer.WriteString(page.Html)
	self.writer.WriteString("\t")
	self.writer.WriteString(page.RespInfo)
	self.writer.WriteString("\n")

	query := extractSearchQuery(page.Link)
	if len(query) > 0 {
		setStatus(query, "downloader.write." + ExtractDomainOnly(page.Link))
	}
	log.Println(time.Now().Unix(), "downloader", "write", page.Link)
}

func (self *DownloadHandler) FlushPages() {
	for page := range self.PageChannel {
		self.WritePage(page)
		self.flushFileSize += 1

		writePageFreq := ConfigInstance().WritePageFreq
		if writePageFreq > 0 && self.flushFileSize%writePageFreq == 0 {
			self.writer.Close()
			self.currentPath = strconv.FormatInt(time.Now().UnixNano(), 10) + ".tsv"
			var err error
			self.writer, err = os.Create("./pages/" + self.currentPath)
			if err != nil {
				log.Println(err)
				os.Exit(0)
			}
			self.flushFileSize = 0
		}
	}
}

func (self *DownloadHandler) GetProxyDownloader() *HTTPGetDownloader {
	if len(self.ProxyDownloader) == 0 {
		return nil
	}
	return self.ProxyDownloader[rand.Intn(len(self.ProxyDownloader))]
}

func (self *DownloadHandler) GetRtDownloaderAddr() string {
	if len(self.RtDownloaderAddrs) == 0 {
		return ""
	}
	return self.RtDownloaderAddrs[rand.Intn(len(self.RtDownloaderAddrs))]
}

func (self *DownloadHandler) ProcessLink(link Link) {
	if !IsValidLink(link.LinkURL) {
		return
	}
	query := extractSearchQuery(link.LinkURL)
	if len(query) > 0 {
		setStatus(query, "downloader.start." + ExtractDomainOnly(link.LinkURL))
	}
	log.Println(time.Now().Unix(), "downloader", "start", link.LinkURL)
	self.processedPageCount += 1
	html := ""
	resp := ""
	var err error
	start := time.Now()
	
	rtd := self.GetRtDownloaderAddr()
	if rtd != "" && len(query) > 0 {
		log.Println("realtime downloader", rtd, "for query", query)
		html, resp, err = self.Downloader.Download(rtd + base64.URLEncoding.EncodeToString([]byte(link.LinkURL)))
	}
	if err != nil || len(html) == 0 {
		html, resp, err = self.Downloader.Download(link.LinkURL)
		if err != nil {
			log.Println(time.Now().Unix(), "downloader", "self_failed", link.LinkURL, err)
			for k := 0; k < 2; k++ {
				downloader := self.GetProxyDownloader()
				if downloader != nil {
					html, resp, err = self.GetProxyDownloader().Download(link.LinkURL)
					if err != nil {
						log.Println(time.Now().Unix(), "downloader", "proxy_failed", link.LinkURL, err)
						self.proxyDownloadedPageFailedCount += 1
					} else {
						self.proxyDownloadedPageCount += 1
						log.Println(time.Now().Unix(), "downloader", "proxy_success", link.LinkURL)
						break
					}
				}
			}
		}
	}

	elapsed := int64(time.Since(start) / 1000000)
	self.metricSender.Timing("crawler.downloader."+GetHostName()+"."+Port+".download_time", elapsed, 1.0)
	self.metricSender.Timing("crawler.downloader.download_time", elapsed, 1.0)

	if err != nil {
		return
	}
	self.totalDownloadedPageCount += 1

	if len(html) < 100 {
		return
	}

	if !IsChinesePage(html) {
		return
	}
	log.Println(time.Now().Unix(), "downloader", "finish", link)
	resp += "<refer>" + link.Referrer + "</refer>"
	page := WebPage{Link: link.LinkURL, Html: html, RespInfo: resp, DownloadedAt: time.Now().Unix()}

	if len(self.PageChannel) < DOWNLOADER_QUEUE_SIZE {
		self.PageChannel <- page
	}

	if ConfigInstance().ExtractLinks == 1 && self.Match(link.LinkURL) < PRIORITY_LEVELS - 1 {
		elinks := ExtractLinks([]byte(html), link.LinkURL)
		for _, elink := range elinks {
			nlink := NormalizeLink(elink)
			linkPriority := self.Match(nlink)
			if linkPriority <= 0 {
				continue
			}
			if IsValidLink(nlink) && len(self.ExtractedLinksChannel) < DOWNLOADER_QUEUE_SIZE {
				self.ExtractedLinksChannel <- Link{LinkURL: nlink, Referrer: link.LinkURL}
			}
		}
	}
}

func (self *DownloadHandler) Download() {
	self.flushFileSize = 0
	rand.Seed(time.Now().UnixNano())
	for link0 := range self.LinksChannel {
		go self.ProcessLink(link0)
	}
}

func (self *DownloadHandler) Match(link string) int {
	return self.urlFilter.Match(link)
}

func (self *DownloadHandler) ProcExtractedLinks() {
	procn := 0
	tm := time.Now().Unix()
	lm := make(map[string]Link)
	for link := range self.ExtractedLinksChannel {
		lm[link.LinkURL] = link
		tm1 := time.Now().Unix()

		if tm1-tm > 60 || len(lm) > 100 || procn < 10 {
			pb := PostBody{}
			pb.Links = []Link{}
			for _, lk := range lm {
				pb.Links = append(pb.Links, lk)
			}
			jsonBlob, err := json.Marshal(&pb)
			if err == nil {
				req := make(map[string]string)
				req["links"] = string(jsonBlob)
				PostHTTPRequest(ConfigInstance().RedirectorHost, req)
			}
			tm = time.Now().Unix()
			lm = make(map[string]Link)
		}
		procn += 1
	}
}

func NewDownloadHanler() *DownloadHandler {
	ret := DownloadHandler{}
	ret.urlFilter = NewURLFilter()
	var err error
	ret.currentPath = strconv.FormatInt(time.Now().UnixNano(), 10) + ".tsv"
	ret.writer, err = os.Create("./pages/" + ret.currentPath)

	if err != nil {
		os.Exit(0)
	}
	ret.RtDownloaderAddrs = GetRealtimeDownloaderList()
	ret.metricSender, _ = graphite.New(ConfigInstance().GraphiteHost, "")
	ret.LinksChannel = make(chan Link, DOWNLOADER_QUEUE_SIZE)
	ret.PageChannel = make(chan WebPage, DOWNLOADER_QUEUE_SIZE)
	ret.ExtractedLinksChannel = make(chan Link, DOWNLOADER_QUEUE_SIZE)
	ret.Downloader = NewHTTPGetDownloader()
	ret.processedPageCount = 0
	ret.totalDownloadedPageCount = 0
	ret.proxyDownloadedPageCount = 0
	ret.writePageCount = 0
	ret.linkRecvCount = make(map[string]int)
	ret.pageDownloadCount = make(map[string]int)
	ret.pageWriteCount = make(map[string]int)

	for _, proxy := range GetProxyList() {
		pd := NewHTTPGetProxyDownloader(proxy)
		if pd == nil {
			continue
		}
		ret.ProxyDownloader = append(ret.ProxyDownloader, pd)
	}
	log.Println("proxy downloader count", len(ret.ProxyDownloader))

	ret.signals = make(chan os.Signal, 1)
	signal.Notify(ret.signals, syscall.SIGINT)
	go func() {
		<-ret.signals
		defer ret.writer.Close()
		os.Exit(0)
	}()
	go ret.Download()
	go ret.ProcExtractedLinks()
	go ret.FlushPages()
	return &ret
}

func (self *DownloadHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	defer func() {
		if r := recover(); r != nil {
			log.Println(r)
		}
	}()

	links := req.PostFormValue("links")
	if len(links) > 0 {
		pb := PostBody{}
		json.Unmarshal([]byte(links), &pb)

		for _, link := range pb.Links {
			if len(self.LinksChannel) < DOWNLOADER_QUEUE_SIZE {
				self.LinksChannel <- link
			}
		}
	}

	ret := Response{
		PostChannelLength:      len(self.LinksChannel),
		ExtractedChannelLength: len(self.ExtractedLinksChannel),
	}
	if rand.Float64() < 0.1 {

		self.metricSender.Gauge("crawler.downloader."+GetHostName()+"."+Port+".postchannelsize", int64(ret.PostChannelLength), 1.0)
		self.metricSender.Gauge("crawler.downloader."+GetHostName()+"."+Port+".extractchannelsize", int64(ret.ExtractedChannelLength), 1.0)
		self.metricSender.Gauge("crawler.downloader."+GetHostName()+"."+Port+".cachesize", int64(self.flushFileSize), 1.0)
		self.metricSender.Gauge("crawler.downloader."+GetHostName()+"."+Port+".pagechannelsize", int64(len(self.PageChannel)), 1.0)
		self.metricSender.Gauge("crawler.downloader."+GetHostName()+"."+Port+".totalDownloadedPageCount", int64(self.totalDownloadedPageCount), 1.0)
		self.metricSender.Gauge("crawler.downloader."+GetHostName()+"."+Port+".processedPageCount", int64(self.processedPageCount), 1.0)
		self.metricSender.Gauge("crawler.downloader."+GetHostName()+"."+Port+".proxyDownloadedPageCount", int64(self.proxyDownloadedPageCount), 1.0)
		self.metricSender.Gauge("crawler.downloader."+GetHostName()+"."+Port+".proxyDownloadedPageFailedCount", int64(self.proxyDownloadedPageFailedCount), 1.0)
		self.metricSender.Gauge("crawler.downloader."+GetHostName()+"."+Port+".writePageCount", int64(self.writePageCount), 1.0)
	}
	output, _ := json.Marshal(&ret)
	fmt.Fprint(w, string(output))
}
