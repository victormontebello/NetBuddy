// Package speedtest interfaces for testing internet bandwidth through HTTP by speedtest.net
package speedtest

import (
	"encoding/xml"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type byDistance []Server
type ST struct {
	cfg     Settings
	servers []Server
}
type Client struct {
	IP  string  `xml:"ip,attr"`
	Lat float64 `xml:"lat,attr"`
	Lon float64 `xml:"lon,attr"`
	ISP string  `xml:"isp,attr"`
}
type Server struct {
	Name     string  `xml:"name,attr"`
	Id       string  `xml:"id,attr"`
	Sponsor  string  `xml:"sponsor,attr"`
	Country  string  `xml:"country,attr"`
	URL      string  `xml:"url,attr"`
	URL2     string  `xml:"url2,attr"`
	Lat      float64 `xml:"lat,attr"`
	Lon      float64 `xml:"lon,attr"`
	Distance float64
}

type Hosts struct {
	Server []Server `xml:"servers>server"`
}

type Settings struct {
	Download struct {
		TestLength    float64 `xml:"testlength,attr"`
		ThreadsPerURL int     `xml:"threadsperurl,attr"`
	} `xml:"download"`
	Upload struct {
		Ratio         int     `xml:"ratio,attr"`
		MaxChunkCount int     `xml:"maxchunkcount,attr"`
		Threads       int     `xml:"threads,attr"`
		TestLength    float64 `xml:"testlength,attr"`
	} `xml:"upload"`
	ServerCfg struct {
		IgnoreIds string `xml:"ignoreids,attr"`
	} `xml:"server-config"`
	Client struct {
		Client
	} `xml:"client"`
}

func Run() error {
	st := ST{}
	fmt.Printf("Powered by Ookla — http://www.speedtest.net/terms\n")
	fmt.Printf("Downloading the speedtest.net configuration")
	if err := st.getConfig(); err != nil {
		return err
	}
	fmt.Printf(" \u2713\nYour IP: %s, Service Provider: %s\n", st.cfg.Client.IP, st.cfg.Client.ISP)
	fmt.Printf("Retrieving speedtest.net servers list")
	if err := st.getServers(); err != nil {
		return err
	}
	fmt.Printf(" \u2713\nSelecting best server based on Geo and Latency")
	server, latency := st.bestServer()
	if server.Distance == 0 {
		return fmt.Errorf("could not find a server")
	}
	fmt.Printf(" \u2713\nHosted by %s (%s) %.2f ms, %.0f miles\n",
		server.Sponsor,
		server.Name,
		latency*1000,
		server.Distance)
	down := st.download(server)
	fmt.Printf("Download: %s\n", down)
	up := st.upload(server)
	fmt.Printf("Upload: %s\n", up)
	return nil
}

func getData(url string) ([]byte, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return []byte{}, err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Connection", "keep-alive")

	resp, err := client.Do(req)
	if err != nil {
		return []byte{}, err
	}
	if resp.StatusCode != 200 {
		return []byte{}, fmt.Errorf("can not connect")
	}
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return b, err
	}
	return b, nil
}

func (st *ST) getConfig() error {
	b, err := getData("http://www.speedtest.net/speedtest-config.php")
	if err != nil {
		return err
	}
	xml.Unmarshal(b, &st.cfg)
	return nil
}

func (st *ST) getServers() error {
	var (
		isIgnoreId = make(map[string]struct{})
		stServers  = []string{
			"http://www.speedtest.net/speedtest-servers-static.php",
			"http://c.speedtest.net/speedtest-servers-static.php",
		}
		hosts Hosts
	)

	for _, server := range stServers {
		b, err := getData(server)
		if err != nil {
			continue
		}

		xml.Unmarshal(b, &hosts)

		st.cfg.Client.Lon = st.cfg.Client.Lon * math.Pi / 180
		st.cfg.Client.Lat = st.cfg.Client.Lat * math.Pi / 180

		for _, ignoreId := range strings.Split(st.cfg.ServerCfg.IgnoreIds, ",") {
			isIgnoreId[ignoreId] = struct{}{}
		}

		for i, server := range hosts.Server {
			if _, ok := isIgnoreId[hosts.Server[i].Id]; !ok {
				hosts.Server[i].Distance = distance(st.cfg.Client.Lon, st.cfg.Client.Lat, server)
				st.servers = append(st.servers, hosts.Server[i])
			}
		}

		sort.Sort(byDistance(st.servers))
		break
	}
	return nil
}

func (st *ST) bestServer() (Server, float64) {
	var (
		latency float64
		sum     float64
		server  Server
	)
	latency = 1000
	for i := range []int{1, 2, 3, 4} {
		base := st.servers[i].URL[:strings.LastIndex(st.servers[i].URL, "/")]
		url := base + "/latency.txt"
		sum = 0
		for range []int{1, 2} {
			ts := time.Now()
			_, err := getData(url)
			if err != nil {
				sum = 100000.0
				break
			}
			elapsed := time.Since(ts)
			sum += elapsed.Seconds()
		}
		if sum/2 < latency {
			server = st.servers[i]
			latency = sum / 2
		}
	}
	return server, latency
}

func (st *ST) download(server Server) string {
	var (
		wg        sync.WaitGroup
		urls      []string
		totalRcvd float64
		sizeDld   = []int{350, 500, 750, 1000, 1500, 2000, 2500, 3000, 3500, 4000}
	)

	base := server.URL[:strings.LastIndex(server.URL, "/")]

	for _, size := range sizeDld {
		for i := 0; i < st.cfg.Download.ThreadsPerURL; i++ {
			urls = append(urls, fmt.Sprintf("%s/random%dx%d.jpg", base, size, size))
		}
	}
	ts := time.Now()
	fmt.Printf("Testing download ")
	for _, url := range urls {
		wg.Add(1)
		go func(url string) {
			var (
				buf   = make([]byte, 10240)
				total int
			)

			defer wg.Done()
			timeout := time.Duration(st.cfg.Download.TestLength) * time.Second
			client := http.Client{
				Timeout: timeout,
			}
			resp, _ := client.Get(url)
			ts := time.Now()
			for {
				lr := io.LimitReader(resp.Body, 10240)
				n, err := io.ReadFull(lr, buf)
				total += n
				if n == 0 || err != nil {
					break
				}
				if st.cfg.Download.TestLength < time.Since(ts).Seconds() {
					break
				}
			}
			totalRcvd += float64(total)
			fmt.Printf(".")
		}(url)
	}
	wg.Wait()
	fmt.Printf("\n")
	return fmtUnit(totalRcvd / time.Since(ts).Seconds())
}

func (st *ST) upload(server Server) string {
	var (
		sizes       []int
		totalUpload int
		sizeUpl     = []int{32768, 65536, 131072, 262144, 524288, 1048576, 7340032}
		count       = st.cfg.Upload.MaxChunkCount * 2 / len(sizeUpl[st.cfg.Upload.Ratio-1:])
		jobCh       = make(chan int, len(sizeUpl)*count)
		resCh       = make(chan int, len(sizeUpl)*count)
		done        = make(chan struct{})
		base        = server.URL[:strings.LastIndex(server.URL, "/")]
	)

	for _, size := range sizeUpl[st.cfg.Upload.Ratio-1:] {
		for i := 0; i < count; i++ {
			sizes = append(sizes, size)
		}
	}
	// TODO: needs optimization
	fmt.Printf("Testing upload ")
	for t := 0; t < st.cfg.Upload.MaxChunkCount; t++ {
		go func() {
			defer func() {
				recover()
			}()
			tr := &http.Transport{
				DisableKeepAlives:  false,
				DisableCompression: true,
			}
			client := &http.Client{
				Transport: tr,
			}
			req, err := http.NewRequest("POST", base+"/upload.php", nil)
			if err != nil {
				return
			}
			req.Header.Set("User-Agent", "Mozilla/5.0")
			testLength := 32
			chunkData := strings.Repeat("0123456789ABCDEFGHIJKLMNOPQRSTUV", testLength)
		DONE:
			for {
				size := <-jobCh
				chunkNums := size / testLength
				ts := time.Now()
				c := 0
			LOOP:
				for time.Since(ts).Seconds() < st.cfg.Upload.TestLength {
					buf := strings.NewReader(chunkData)
					req.Body = ioutil.NopCloser(buf)
					resp, err := client.Do(req)
					if err != nil || resp.StatusCode != 200 {
						println(err.Error())
						continue
					}

					io.Copy(ioutil.Discard, resp.Body)
					resp.Body.Close()

					resCh <- testLength
					chunkNums--
					if chunkNums > 0 {
						c++
					} else {
						break LOOP
					}
					select {
					case <-done:
						break DONE
					default:
						continue
					}
				}
				fmt.Printf(".")
			}
		}()
	}

	ts := time.Now()

	go func() {
		for {
			chunk, ok := <-resCh
			if !ok {
				break
			}
			totalUpload += chunk
		}
	}()

	fmt.Printf("\n")
	return fmtUnit(float64(totalUpload) / time.Since(ts).Seconds())
}

func findExt() {
}

func fmtUnit(n float64) string {
	n = n * 8
	if n > 1e9 {
		return fmt.Sprintf("%.2f Gbps", n/1e9)
	} else if n > 1e6 {
		return fmt.Sprintf("%.2f Mbps", n/1e6)
	}
	return fmt.Sprintf("%.2f Kbps", n/1e3)
}

func distance(cLon, cLat float64, server Server) float64 {
	server.Lon = server.Lon * math.Pi / 180
	server.Lat = server.Lat * math.Pi / 180

	deltaLon := server.Lon - cLon
	deltaLat := server.Lat - cLat

	hsinLat := math.Pow(math.Sin(deltaLat/2), 2)
	hsinLon := math.Pow(math.Sin(deltaLon/2), 2)

	a := hsinLat + math.Cos(server.Lat)*math.Cos(cLat)*hsinLon
	c := 2 * 3961 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return c
}

func (a byDistance) Len() int           { return len(a) }
func (a byDistance) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byDistance) Less(i, j int) bool { return a[i].Distance < a[j].Distance }
