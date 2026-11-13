package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// The window shows how the phone is doing and changes its switches (wgcore: GET and POST /phone). It needs the
// phone's token, which only the tunnel's keys can read from the phone's sealed answer; the watcher service has them
// and leaves the token here for the window, which runs without administrator rights. It lets one do no more than
// the window shows: read the phone's state, switch its options, hand it a signed release.

// bare is a failed request's error without the address that was asked: the address carries the phone's token, which
// belongs in no log and on no screen.
func bare(err error) error {
	var u *url.Error
	if errors.As(err, &u) {
		return u.Err
	}
	return err
}

func phoneAccessPath() string { return filepath.Join(dataDir(), "phone.json") }

type phoneAccess struct {
	Token string    `json:"token"`
	At    time.Time `json:"at"`
}

func savePhoneAccess(token string) {
	if token == "" {
		return
	}
	if old := loadPhoneAccess(); old.Token == token && time.Since(old.At) < time.Hour {
		return
	}
	if data, err := json.Marshal(phoneAccess{Token: token, At: time.Now()}); err == nil {
		_ = os.WriteFile(phoneAccessPath(), data, 0o644)
	}
}

func loadPhoneAccess() phoneAccess {
	var a phoneAccess
	if data, err := os.ReadFile(phoneAccessPath()); err == nil {
		_ = json.Unmarshal(data, &a)
	}
	return a
}

// phoneDevice is one device as the phone tells it.
type phoneDevice struct {
	Name      string `json:"name"`
	Today     int64  `json:"today"`
	LimitMB   int64  `json:"limit_mb"`
	SpeedKbps int64  `json:"speed_kbps"`
	Quiet     string `json:"quiet"`  // "23:00–07:00", or empty
	Guest     string `json:"guest"`  // "Misafir · 45 dk kaldı", or empty
	Paused    string `json:"paused"` // "none", "manual", "limit" or "quiet"
}

// phonePlan is the phone's monthly plan.
type phonePlan struct {
	LimitMB    int64 `json:"limit_mb"`
	Used       int64 `json:"used"`
	DaysLeft   int   `json:"days_left"`
	PerDay     int64 `json:"per_day"`
	WholePhone bool  `json:"whole_phone"`
}

// phoneSpeed is the phone's last speed test of its own internet; rates in bits per second.
type phoneSpeed struct {
	Running bool   `json:"running"`
	Error   string `json:"error"`
	DownBps int64  `json:"down_bps"`
	UpBps   int64  `json:"up_bps"`
	PingMs  int64  `json:"ping_ms"`
	Bytes   int64  `json:"bytes"`
	At      int64  `json:"at"`
}

// phoneStatus is the phone's state (see Controls.status in the app).
type phoneStatus struct {
	Version   string        `json:"version"`
	Code      int           `json:"code"` // the app's release number (apps from 1.2.2 say it)
	Battery   int           `json:"battery"`
	Charging  bool          `json:"charging"`
	Network   string        `json:"network"`
	Signal    int           `json:"signal"`
	Today     int64         `json:"today"`
	LimitMB   int64         `json:"limit_mb"`
	DownBps   int64         `json:"down_bps"`
	UpBps     int64         `json:"up_bps"`
	Blocked   int64         `json:"blocked"`
	Split     bool          `json:"split"`
	Ads       bool          `json:"ads"`
	Saver     bool          `json:"saver"`
	Priority  bool          `json:"priority"`
	Guard     bool          `json:"guard"`
	Hot       bool          `json:"hot"`
	Band      string        `json:"band"`
	Devices   []phoneDevice `json:"devices"`
	Plan      *phonePlan    `json:"plan,omitempty"`
	SpeedTest *phoneSpeed   `json:"speedtest,omitempty"`
}

var phoneClient = &http.Client{Timeout: 2 * time.Second}

// phoneURL is where the phone's state is: through the tunnel when it is up (nothing else gets out then), else on
// the phone's Wi-Fi network.
func phoneURL(set settings, viaTunnel bool, token string) string {
	host := set.Phone
	if viaTunnel {
		host = tunnelGateway
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(updatePort)) + "/phone?t=" + token
}

func fetchPhone(set settings, viaTunnel bool) (*phoneStatus, error) {
	a := loadPhoneAccess()
	if a.Token == "" {
		return nil, errors.New("telefonun belirteci henüz yok")
	}
	resp, err := phoneClient.Get(phoneURL(set, viaTunnel, a.Token))
	if err != nil {
		return nil, bare(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("telefon %s dedi", resp.Status)
	}
	var s phoneStatus
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&s); err != nil {
		return nil, err
	}
	return &s, nil
}

// The Wi-Fi side of the speed test: this computer to the phone and back, through the tunnel when it is up (the
// way the shared traffic goes), and nothing of it on the mobile network (wgcore: GET and POST /speed).
const (
	wifiTestTime  = 4 * time.Second
	wifiTestBytes = 400 << 20 // at most, each way; the time ends it first on any Wi-Fi
)

// measureWifi returns the download and upload rates between this computer and the phone, bits per second.
func measureWifi(set settings, viaTunnel bool) (down, up int64, err error) {
	a := loadPhoneAccess()
	if a.Token == "" {
		return 0, 0, errors.New("telefona henüz ulaşılamadı")
	}
	host := set.Phone
	if viaTunnel {
		host = tunnelGateway
	}
	url := "http://" + net.JoinHostPort(host, strconv.Itoa(updatePort)) + "/speed?t=" + a.Token
	client := &http.Client{Timeout: wifiTestTime + 10*time.Second}

	resp, err := client.Get(url + "&bytes=" + strconv.Itoa(wifiTestBytes))
	if err != nil {
		return 0, 0, fmt.Errorf("telefona ulaşılamadı: %w", bare(err))
	}
	start := time.Now()
	n, _ := io.Copy(io.Discard, &deadlineReader{r: resp.Body, until: start.Add(wifiTestTime)})
	elapsed := time.Since(start)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || n == 0 {
		return 0, 0, fmt.Errorf("telefon hız ölçümünü bilmiyor (%s): önce telefonu güncelle", resp.Status)
	}
	down = int64(float64(n) * 8 / elapsed.Seconds())

	body := &deadlineReader{r: zeroReader{}, until: time.Now().Add(wifiTestTime), limit: wifiTestBytes}
	resp, err = client.Post(url, "application/octet-stream", body)
	if err != nil {
		return down, 0, fmt.Errorf("telefona ulaşılamadı: %w", bare(err))
	}
	var got struct{ Bytes, Ms int64 }
	err = json.NewDecoder(io.LimitReader(resp.Body, 1<<10)).Decode(&got)
	resp.Body.Close()
	if err != nil || got.Ms <= 0 {
		return down, 0, errors.New("telefon yüklemeyi ölçemedi")
	}
	return down, got.Bytes * 8 * 1000 / got.Ms, nil
}

// deadlineReader ends a stream at a time, or after limit bytes when limit is set.
type deadlineReader struct {
	r     io.Reader
	until time.Time
	limit int64
	read  int64
}

func (d *deadlineReader) Read(p []byte) (int, error) {
	if time.Now().After(d.until) || (d.limit > 0 && d.read >= d.limit) {
		return 0, io.EOF
	}
	if d.limit > 0 {
		p = p[:min(int64(len(p)), d.limit-d.read)]
	}
	n, err := d.r.Read(p)
	d.read += int64(n)
	return n, err
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

// sendPhone asks the phone for a change ({"split": true}, {"pause": "Tab"}…); it applies it within a few seconds.
func sendPhone(set settings, viaTunnel bool, change map[string]any) error {
	a := loadPhoneAccess()
	if a.Token == "" {
		return errors.New("telefona henüz ulaşılamadı")
	}
	data, err := json.Marshal(change)
	if err != nil {
		return err
	}
	resp, err := phoneClient.Post(phoneURL(set, viaTunnel, a.Token), "application/json", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("telefona ulaşılamadı: %w", bare(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telefon %s dedi", resp.Status)
	}
	return nil
}
