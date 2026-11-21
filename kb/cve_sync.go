package kb

// Reconstructed (D:/ARTEX/kb/cve_sync.go).
// CVE 语料同步：NVD JSON feed → upsert 进 cve.db。

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type nvdItem struct {
	Cve struct {
		ID           string `json:"id"`
		Published    string `json:"published"`
		Descriptions []struct {
			Lang  string `json:"lang"`
			Value string `json:"value"`
		} `json:"descriptions"`
		Weaknesses []struct {
			Description []struct {
				Value string `json:"value"`
			} `json:"description"`
		} `json:"weaknesses"`
		Metrics json.RawMessage `json:"metrics"`
		Refs    []struct {
			URL  string   `json:"url"`
			Tags []string `json:"tags"`
		} `json:"references"`
	} `json:"cve"`
}

func nvdToRecord(it *nvdItem) *CVERecord {
	r := &CVERecord{ID: it.Cve.ID}
	if len(it.Cve.Published) >= 4 {
		fmt.Sscanf(it.Cve.Published[:4], "%d", &r.Year)
	}
	for _, d := range it.Cve.Descriptions {
		if d.Lang == "en" {
			r.Description = d.Value
		}
	}
	for _, w := range it.Cve.Weaknesses {
		for _, d := range w.Description {
			r.CWE = d.Value
		}
	}
	var refs []string
	for _, ref := range it.Cve.Refs {
		refs = append(refs, ref.URL)
		for _, t := range ref.Tags {
			if t == "Exploit" {
				r.HasExploit = true
			}
		}
	}
	r.Refs = strings.Join(refs, " ")
	return r
}

func (c *CVEClient) upsertCVE(r *CVERecord) error {
	_, err := c.db.Exec(`INSERT INTO cves(id,year,description,cwe,severity,has_exploit,refs)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET description=excluded.description,
		cwe=excluded.cwe,severity=excluded.severity,
		has_exploit=excluded.has_exploit,refs=excluded.refs`,
		r.ID, r.Year, r.Description, r.CWE, r.Severity, b2i(r.HasExploit), r.Refs)
	return err
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// SyncCVE 从 NVD feed URL 同步增量窗口（默认近 120 天）。
func SyncCVE(dir, feedURL string) (int, error) {
	c, err := OpenCVE(dir)
	if err != nil {
		return 0, err
	}
	defer c.Close()
	if feedURL == "" {
		feedURL = "https://nvd.nist.gov/feeds/json/cve/1.1/nvdcve-1.1-modified.json.gz"
	}
	cl := &http.Client{Timeout: 120 * time.Second}
	resp, err := cl.Get(feedURL)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var rdr io.Reader = resp.Body
	if strings.HasSuffix(feedURL, ".gz") {
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return 0, err
		}
		defer gz.Close()
		rdr = gz
	}
	var feed struct {
		Items []nvdItem `json:"CVE_Items"`
	}
	if err := json.NewDecoder(rdr).Decode(&feed); err != nil {
		return 0, err
	}
	n := 0
	for _, it := range feed.Items {
		if r := nvdToRecord(&it); r != nil {
			if err := c.upsertCVE(r); err == nil {
				n++
			}
		}
	}
	return n, nil
}

// syncWindow 计算增量窗口（默认近 120 天）。
func syncWindow() (time.Time, time.Time) {
	end := time.Now()
	return end.AddDate(0, 0, -120), end
}
