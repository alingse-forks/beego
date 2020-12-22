// Copyright 2014 beego Author. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package web

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"

	"github.com/beego/beego/v2"
	"github.com/beego/beego/v2/core/config"
	"github.com/beego/beego/v2/core/logs"
	"github.com/beego/beego/v2/server/web/session"

	"github.com/beego/beego/v2/core/utils"
	"github.com/beego/beego/v2/server/web/context"
)

// Config is the main struct for BConfig
// TODO after supporting multiple servers, remove common config to somewhere else
type Config struct {
	AppName             string // Application name
	RunMode             string // Running Mode: dev | prod
	RouterCaseSensitive bool
	ServerName          string
	RecoverPanic        bool
	RecoverFunc         func(*context.Context, *Config)
	CopyRequestBody     bool
	EnableGzip          bool
	// MaxMemory and MaxUploadSize are used to limit the request body
	// if the request is not uploading file, MaxMemory is the max size of request body
	// if the request is uploading file, MaxUploadSize is the max size of request body
	MaxMemory          int64
	MaxUploadSize      int64
	EnableErrorsShow   bool
	EnableErrorsRender bool
	Listen             Listen
	WebConfig          WebConfig
	Log                LogConfig
}

// Listen holds for http and https related config
type Listen struct {
	Graceful          bool // Graceful means use graceful module to start the server
	ServerTimeOut     int64
	ListenTCP4        bool
	EnableHTTP        bool
	HTTPAddr          string
	HTTPPort          int
	AutoTLS           bool
	Domains           []string
	TLSCacheDir       string
	EnableHTTPS       bool
	EnableMutualHTTPS bool
	HTTPSAddr         string
	HTTPSPort         int
	HTTPSCertFile     string
	HTTPSKeyFile      string
	TrustCaFile       string
	EnableAdmin       bool
	AdminAddr         string
	AdminPort         int
	EnableFcgi        bool
	EnableStdIo       bool // EnableStdIo works with EnableFcgi Use FCGI via standard I/O
	ClientAuth        int
}

// WebConfig holds web related config
type WebConfig struct {
	AutoRender             bool
	EnableDocs             bool
	FlashName              string
	FlashSeparator         string
	DirectoryIndex         bool
	StaticDir              map[string]string
	StaticExtensionsToGzip []string
	StaticCacheFileSize    int
	StaticCacheFileNum     int
	TemplateLeft           string
	TemplateRight          string
	ViewsPath              string
	CommentRouterPath      string
	EnableXSRF             bool
	XSRFKey                string
	XSRFExpire             int
	Session                SessionConfig
}

// SessionConfig holds session related config
type SessionConfig struct {
	SessionOn                    bool
	SessionProvider              string
	SessionName                  string
	SessionGCMaxLifetime         int64
	SessionProviderConfig        string
	SessionCookieLifeTime        int
	SessionAutoSetCookie         bool
	SessionDomain                string
	SessionDisableHTTPOnly       bool // used to allow for cross domain cookies/javascript cookies.
	SessionEnableSidInHTTPHeader bool // enable store/get the sessionId into/from http headers
	SessionNameInHTTPHeader      string
	SessionEnableSidInURLQuery   bool // enable get the sessionId from Url Query params
	SessionCookieSameSite        http.SameSite
}

// LogConfig holds Log related config
type LogConfig struct {
	AccessLogs       bool
	EnableStaticLogs bool   // log static files requests default: false
	AccessLogsFormat string // access log format: JSON_FORMAT, APACHE_FORMAT or empty string
	FileLineNum      bool
	Outputs          map[string]string // Store Adaptor : config
}

var (
	// BConfig is the default config for Application
	BConfig *Config
	// AppConfig is the instance of Config, store the config information from file
	AppConfig *beegoAppConfig
	// AppPath is the absolute path to the app
	AppPath string
	// GlobalSessions is the instance for the session manager
	GlobalSessions *session.Manager

	// appConfigPath is the path to the config files
	appConfigPath string
	// appConfigProvider is the provider for the config, default is ini
	appConfigProvider = "ini"
	// WorkPath is the absolute path to project root directory
	WorkPath string
)

func init() {
	BConfig = newBConfig()
	var err error
	if AppPath, err = filepath.Abs(filepath.Dir(os.Args[0])); err != nil {
		panic(err)
	}
	WorkPath, err = os.Getwd()
	if err != nil {
		panic(err)
	}
	var filename = "app.conf"
	if os.Getenv("BEEGO_RUNMODE") != "" {
		filename = os.Getenv("BEEGO_RUNMODE") + ".app.conf"
	}
	appConfigPath = filepath.Join(WorkPath, "conf", filename)
	if !utils.FileExists(appConfigPath) {
		appConfigPath = filepath.Join(AppPath, "conf", filename)
		if !utils.FileExists(appConfigPath) {
			AppConfig = &beegoAppConfig{innerConfig: config.NewFakeConfig()}
			return
		}
	}
	if err = parseConfig(appConfigPath); err != nil {
		panic(err)
	}
}

func defaultRecoverPanic(ctx *context.Context, cfg *Config) {
	if err := recover(); err != nil {
		if err == ErrAbort {
			return
		}
		if !cfg.RecoverPanic {
			panic(err)
		}
		if cfg.EnableErrorsShow {
			if _, ok := ErrorMaps[fmt.Sprint(err)]; ok {
				exception(fmt.Sprint(err), ctx)
				return
			}
		}
		var stack string
		logs.Critical("the request url is ", ctx.Input.URL())
		logs.Critical("Handler crashed with error", err)
		for i := 1; ; i++ {
			_, file, line, ok := runtime.Caller(i)
			if !ok {
				break
			}
			logs.Critical(fmt.Sprintf("%s:%d", file, line))
			stack = stack + fmt.Sprintln(fmt.Sprintf("%s:%d", file, line))
		}
		if cfg.RunMode == DEV && cfg.EnableErrorsRender {
			showErr(err, ctx, stack)
		}
		if ctx.Output.Status != 0 {
			ctx.ResponseWriter.WriteHeader(ctx.Output.Status)
		} else {
			ctx.ResponseWriter.WriteHeader(500)
		}
	}
}

func newBConfig() *Config {
	res := &Config{
		AppName:             "beego",
		RunMode:             PROD,
		RouterCaseSensitive: true,
		ServerName:          "beegoServer:" + beego.VERSION,
		RecoverPanic:        true,

		CopyRequestBody:    false,
		EnableGzip:         false,
		MaxMemory:          1 << 26, // 64MB
		MaxUploadSize:      1 << 30, // 1GB
		EnableErrorsShow:   true,
		EnableErrorsRender: true,
		Listen: Listen{
			Graceful:      false,
			ServerTimeOut: 0,
			ListenTCP4:    false,
			EnableHTTP:    true,
			AutoTLS:       false,
			Domains:       []string{},
			TLSCacheDir:   ".",
			HTTPAddr:      "",
			HTTPPort:      8080,
			EnableHTTPS:   false,
			HTTPSAddr:     "",
			HTTPSPort:     10443,
			HTTPSCertFile: "",
			HTTPSKeyFile:  "",
			EnableAdmin:   false,
			AdminAddr:     "",
			AdminPort:     8088,
			EnableFcgi:    false,
			EnableStdIo:   false,
			ClientAuth:    int(tls.RequireAndVerifyClientCert),
		},
		WebConfig: WebConfig{
			AutoRender:             true,
			EnableDocs:             false,
			FlashName:              "BEEGO_FLASH",
			FlashSeparator:         "BEEGOFLASH",
			DirectoryIndex:         false,
			StaticDir:              map[string]string{"/static": "static"},
			StaticExtensionsToGzip: []string{".css", ".js"},
			StaticCacheFileSize:    1024 * 100,
			StaticCacheFileNum:     1000,
			TemplateLeft:           "{{",
			TemplateRight:          "}}",
			ViewsPath:              "views",
			CommentRouterPath:      "controllers",
			EnableXSRF:             false,
			XSRFKey:                "beegoxsrf",
			XSRFExpire:             0,
			Session: SessionConfig{
				SessionOn:                    false,
				SessionProvider:              "memory",
				SessionName:                  "beegosessionID",
				SessionGCMaxLifetime:         3600,
				SessionProviderConfig:        "",
				SessionDisableHTTPOnly:       false,
				SessionCookieLifeTime:        0, // set cookie default is the browser life
				SessionAutoSetCookie:         true,
				SessionDomain:                "",
				SessionEnableSidInHTTPHeader: false, // enable store/get the sessionId into/from http headers
				SessionNameInHTTPHeader:      "Beegosessionid",
				SessionEnableSidInURLQuery:   false, // enable get the sessionId from Url Query params
				SessionCookieSameSite:        http.SameSiteDefaultMode,
			},
		},
		Log: LogConfig{
			AccessLogs:       false,
			EnableStaticLogs: false,
			AccessLogsFormat: "APACHE_FORMAT",
			FileLineNum:      true,
			Outputs:          map[string]string{"console": ""},
		},
	}

	res.RecoverFunc = defaultRecoverPanic
	return res
}

// now only support ini, next will support json.
func parseConfig(appConfigPath string) (err error) {
	AppConfig, err = newAppConfig(appConfigProvider, appConfigPath)
	if err != nil {
		return err
	}
	return assignConfig(AppConfig)
}

// assignConfig is tricky.
// For 1.x, it use assignSingleConfig to parse the file
// but for 2.x, we use Unmarshaler method
func assignConfig(ac config.Configer) error {

	parseConfigForV1(ac)

	err := ac.Unmarshaler("", BConfig)
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, fmt.Sprintf("Unmarshaler config file to BConfig failed. "+
			"And if you are working on v1.x config file, please ignore this, err: %s", err))
		return err
	}

	// init log
	logs.Reset()
	for adaptor, cfg := range BConfig.Log.Outputs {
		err := logs.SetLogger(adaptor, cfg)
		if err != nil {
			fmt.Fprintln(os.Stderr, fmt.Sprintf("%s with the config %q got err:%s", adaptor, cfg, err.Error()))
			return err
		}
	}
	logs.SetLogFuncCall(BConfig.Log.FileLineNum)
	return nil
}

func parseConfigForV1(ac config.Configer) {
	for _, i := range []interface{}{BConfig, &BConfig.Listen, &BConfig.WebConfig, &BConfig.Log, &BConfig.WebConfig.Session} {
		assignSingleConfig(i, ac)
	}

	// set the run mode first
	if envRunMode := os.Getenv("BEEGO_RUNMODE"); envRunMode != "" {
		BConfig.RunMode = envRunMode
	} else if runMode, err := ac.String("RunMode"); runMode != "" && err == nil {
		BConfig.RunMode = runMode
	}

	if sd, err := ac.String("StaticDir"); sd != "" && err == nil {
		BConfig.WebConfig.StaticDir = map[string]string{}
		sds := strings.Fields(sd)
		for _, v := range sds {
			if url2fsmap := strings.SplitN(v, ":", 2); len(url2fsmap) == 2 {
				BConfig.WebConfig.StaticDir["/"+strings.Trim(url2fsmap[0], "/")] = url2fsmap[1]
			} else {
				BConfig.WebConfig.StaticDir["/"+strings.Trim(url2fsmap[0], "/")] = url2fsmap[0]
			}
		}
	}

	if sgz, err := ac.String("StaticExtensionsToGzip"); sgz != "" && err == nil {
		extensions := strings.Split(sgz, ",")
		fileExts := []string{}
		for _, ext := range extensions {
			ext = strings.TrimSpace(ext)
			if ext == "" {
				continue
			}
			if !strings.HasPrefix(ext, ".") {
				ext = "." + ext
			}
			fileExts = append(fileExts, ext)
		}
		if len(fileExts) > 0 {
			BConfig.WebConfig.StaticExtensionsToGzip = fileExts
		}
	}

	if sfs, err := ac.Int("StaticCacheFileSize"); err == nil {
		BConfig.WebConfig.StaticCacheFileSize = sfs
	}

	if sfn, err := ac.Int("StaticCacheFileNum"); err == nil {
		BConfig.WebConfig.StaticCacheFileNum = sfn
	}

	if lo, err := ac.String("LogOutputs"); lo != "" && err == nil {
		// if lo is not nil or empty
		// means user has set his own LogOutputs
		// clear the default setting to BConfig.Log.Outputs
		BConfig.Log.Outputs = make(map[string]string)
		los := strings.Split(lo, ";")
		for _, v := range los {
			if logType2Config := strings.SplitN(v, ",", 2); len(logType2Config) == 2 {
				BConfig.Log.Outputs[logType2Config[0]] = logType2Config[1]
			} else {
				continue
			}
		}
	}
}

func assignSingleConfig(p interface{}, ac config.Configer) {
	pt := reflect.TypeOf(p)
	if pt.Kind() != reflect.Ptr {
		return
	}
	pt = pt.Elem()
	if pt.Kind() != reflect.Struct {
		return
	}
	pv := reflect.ValueOf(p).Elem()

	for i := 0; i < pt.NumField(); i++ {
		pf := pv.Field(i)
		if !pf.CanSet() {
			continue
		}
		name := pt.Field(i).Name
		switch pf.Kind() {
		case reflect.String:
			pf.SetString(ac.DefaultString(name, pf.String()))
		case reflect.Int, reflect.Int64:
			pf.SetInt(ac.DefaultInt64(name, pf.Int()))
		case reflect.Bool:
			pf.SetBool(ac.DefaultBool(name, pf.Bool()))
		case reflect.Struct:
		default:
			// do nothing here
		}
	}

}

// LoadAppConfig allow developer to apply a config file
func LoadAppConfig(adapterName, configPath string) error {
	absConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}

	if !utils.FileExists(absConfigPath) {
		return fmt.Errorf("the target config file: %s don't exist", configPath)
	}

	appConfigPath = absConfigPath
	appConfigProvider = adapterName

	return parseConfig(appConfigPath)
}

type beegoAppConfig struct {
	config.BaseConfiger
	innerConfig config.Configer
}

func newAppConfig(appConfigProvider, appConfigPath string) (*beegoAppConfig, error) {
	ac, err := config.NewConfig(appConfigProvider, appConfigPath)
	if err != nil {
		return nil, err
	}
	return &beegoAppConfig{innerConfig: ac}, nil
}

func (b *beegoAppConfig) Unmarshaler(prefix string, obj interface{}, opt ...config.DecodeOption) error {
	return b.innerConfig.Unmarshaler(prefix, obj, opt...)
}

func (b *beegoAppConfig) Set(key, val string) error {
	if err := b.innerConfig.Set(BConfig.RunMode+"::"+key, val); err != nil {
		return b.innerConfig.Set(key, val)
	}
	return nil
}

func (b *beegoAppConfig) String(key string) (string, error) {
	if v, err := b.innerConfig.String(BConfig.RunMode + "::" + key); v != "" && err == nil {
		return v, nil
	}
	return b.innerConfig.String(key)
}

func (b *beegoAppConfig) Strings(key string) ([]string, error) {
	if v, err := b.innerConfig.Strings(BConfig.RunMode + "::" + key); len(v) > 0 && err == nil {
		return v, nil
	}
	return b.innerConfig.Strings(key)
}

func (b *beegoAppConfig) Int(key string) (int, error) {
	if v, err := b.innerConfig.Int(BConfig.RunMode + "::" + key); err == nil {
		return v, nil
	}
	return b.innerConfig.Int(key)
}

func (b *beegoAppConfig) Int64(key string) (int64, error) {
	if v, err := b.innerConfig.Int64(BConfig.RunMode + "::" + key); err == nil {
		return v, nil
	}
	return b.innerConfig.Int64(key)
}

func (b *beegoAppConfig) Bool(key string) (bool, error) {
	if v, err := b.innerConfig.Bool(BConfig.RunMode + "::" + key); err == nil {
		return v, nil
	}
	return b.innerConfig.Bool(key)
}

func (b *beegoAppConfig) Float(key string) (float64, error) {
	if v, err := b.innerConfig.Float(BConfig.RunMode + "::" + key); err == nil {
		return v, nil
	}
	return b.innerConfig.Float(key)
}

func (b *beegoAppConfig) DefaultString(key string, defaultVal string) string {
	if v, err := b.String(key); v != "" && err == nil {
		return v
	}
	return defaultVal
}

func (b *beegoAppConfig) DefaultStrings(key string, defaultVal []string) []string {
	if v, err := b.Strings(key); len(v) != 0 && err == nil {
		return v
	}
	return defaultVal
}

func (b *beegoAppConfig) DefaultInt(key string, defaultVal int) int {
	if v, err := b.Int(key); err == nil {
		return v
	}
	return defaultVal
}

func (b *beegoAppConfig) DefaultInt64(key string, defaultVal int64) int64 {
	if v, err := b.Int64(key); err == nil {
		return v
	}
	return defaultVal
}

func (b *beegoAppConfig) DefaultBool(key string, defaultVal bool) bool {
	if v, err := b.Bool(key); err == nil {
		return v
	}
	return defaultVal
}

func (b *beegoAppConfig) DefaultFloat(key string, defaultVal float64) float64 {
	if v, err := b.Float(key); err == nil {
		return v
	}
	return defaultVal
}

func (b *beegoAppConfig) DIY(key string) (interface{}, error) {
	return b.innerConfig.DIY(key)
}

func (b *beegoAppConfig) GetSection(section string) (map[string]string, error) {
	return b.innerConfig.GetSection(section)
}

func (b *beegoAppConfig) SaveConfigFile(filename string) error {
	return b.innerConfig.SaveConfigFile(filename)
}