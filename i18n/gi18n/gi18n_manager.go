// Copyright GoFrame Author(https://goframe.org). All Rights Reserved.
//
// This Source Code Form is subject to the terms of the MIT License.
// If a copy of the MIT was not distributed with this file,
// You can obtain one at https://github.com/gogf/gf.

package gi18n

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"

	"github.com/gogf/gf/internal/intlog"

	"github.com/gogf/gf/os/gfsnotify"

	"github.com/gogf/gf/text/gregex"

	"github.com/gogf/gf/util/gconv"

	"github.com/gogf/gf/encoding/gjson"

	"embed"

	"github.com/gogf/gf/os/gfile"
	"github.com/gogf/gf/os/gres"
)

// Manager, it is concurrent safe, supporting hot reload.
type Manager struct {
	mu      sync.RWMutex
	data    map[string]map[string]string // Translating map.
	pattern string                       // Pattern for regex parsing.
	options Options                      // configuration options.
}

// Options is used for i18n object configuration.
type Options struct {
	Path       string   // I18n files storage path.
	Language   string   // Local language.
	Delimiters []string // Delimiters for variable parsing.
	FS         *embed.FS
}

var (
	// defaultDelimiters defines the default key variable delimiters.
	defaultDelimiters = []string{"{#", "}"}
)

// New creates and returns a new i18n manager.
// The optional parameter <option> specifies the custom options for i18n manager.
// It uses a default one if it's not passed.
func new(options ...Options) *Manager {
	var opts Options
	if len(options) > 0 {
		opts = options[0]
	} else {
		opts = DefaultOptions()
	}
	if len(opts.Delimiters) == 0 {
		opts.Delimiters = defaultDelimiters
	}
	m := &Manager{
		options: opts,
		pattern: fmt.Sprintf(
			`%s(\w+)%s`,
			gregex.Quote(opts.Delimiters[0]),
			gregex.Quote(opts.Delimiters[1]),
		),
	}
	intlog.Printf(`New: %#v`, m)
	return m
}

// EmbedFsOption creates and returns am options with the embeded FS
func EmbedFsOption(fs embed.FS, path string) Options {
	return Options{
		FS:   &fs,
		Path: path,
	}
}

// DefaultOptions creates and returns a default options for i18n manager.
func DefaultOptions() Options {
	var (
		path        = "i18n"
		realPath, _ = gfile.Search(path)
	)
	if realPath != "" {
		path = realPath
		// To avoid of the source path of GF: github.com/gogf/i18n/gi18n
		if gfile.Exists(path + gfile.Separator + "gi18n") {
			path = ""
		}
	}
	return Options{
		Path:       path,
		Language:   "en",
		Delimiters: defaultDelimiters,
	}
}

// SetPath sets the directory path storing i18n files.
func (m *Manager) SetPath(path string) error {
	if gres.Contains(path) {
		m.options.Path = path
	} else {
		realPath, _ := gfile.Search(path)
		if realPath == "" {
			return errors.New(fmt.Sprintf(`%s does not exist`, path))
		}
		m.options.Path = realPath
	}
	intlog.Printf(`SetPath: %s`, m.options.Path)
	return nil
}

// SetLanguage sets the language for translator.
func (m *Manager) SetLanguage(language string) {
	m.options.Language = language
	intlog.Printf(`SetLanguage: %s`, m.options.Language)
}

// SetDelimiters sets the delimiters for translator.
func (m *Manager) SetDelimiters(left, right string) {
	m.pattern = fmt.Sprintf(`%s(\w+)%s`, gregex.Quote(left), gregex.Quote(right))
	intlog.Printf(`SetDelimiters: %v`, m.pattern)
}

// T is alias of Translate for convenience.
func (m *Manager) T(content string, language ...string) string {
	return m.Translate(content, language...)
}

// Tf is alias of TranslateFormat for convenience.
func (m *Manager) Tf(format string, values ...interface{}) string {
	return m.TranslateFormat(format, values...)
}

// Tfl is alias of TranslateFormatLang for convenience.
func (m *Manager) Tfl(language string, format string, values ...interface{}) string {
	return m.TranslateFormatLang(language, format, values...)
}

// TranslateFormat translates, formats and returns the <format> with configured language
// and given <values>.
func (m *Manager) TranslateFormat(format string, values ...interface{}) string {
	return fmt.Sprintf(m.Translate(format), values...)
}

// TranslateFormatLang translates, formats and returns the <format> with configured language
// and given <values>. The parameter <language> specifies custom translation language ignoring
// configured language. If <language> is given empty string, it uses the default configured
// language for the translation.
func (m *Manager) TranslateFormatLang(language string, format string, values ...interface{}) string {
	return fmt.Sprintf(m.Translate(format, language), values...)
}

// Translate translates <content> with configured language.
// The parameter <language> specifies custom translation language ignoring configured language.
func (m *Manager) Translate(content string, language ...string) string {
	m.init()
	m.mu.RLock()
	defer m.mu.RUnlock()
	transLang := m.options.Language
	if len(language) > 0 && language[0] != "" {
		transLang = language[0]
	} else {
		transLang = m.options.Language
	}
	data := m.data[transLang]
	if data == nil {
		return content
	}
	// Parse content as name.
	if v, ok := data[content]; ok {
		return v
	}
	// Parse content as variables container.
	result, _ := gregex.ReplaceStringFuncMatch(
		m.pattern, content,
		func(match []string) string {
			if v, ok := data[match[1]]; ok {
				return v
			}
			return match[0]
		})
	intlog.Printf(`Translate for language: %s`, transLang)
	return result
}

// GetValue retrieves and returns the configured content for given key and specified language.
// It returns an empty string if not found.
func (m *Manager) GetContent(key string, language ...string) string {
	m.init()
	m.mu.RLock()
	defer m.mu.RUnlock()
	transLang := m.options.Language
	if len(language) > 0 && language[0] != "" {
		transLang = language[0]
	} else {
		transLang = m.options.Language
	}
	if data, ok := m.data[transLang]; ok {
		return data[key]
	}
	return ""
}

func (m *Manager) Init() {
	m.init()
}

// init initializes the manager for lazy initialization design.
// The i18n manager is only initialized once.
func (m *Manager) init() {
	m.mu.RLock()
	// If the data is not nil, means it's already initialized.
	if m.data != nil {
		m.mu.RUnlock()
		return
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.options.FS != nil {

		m.data = make(map[string]map[string]string)
		fs.WalkDir(m.options.FS, m.options.Path, func(fsPath string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			var (
				path  string
				name  string
				lang  string
				array []string
			)
			name = fsPath
			path = name
			if len(path) == 0 {
				return nil
			}
			array = strings.Split(path, "/")
			if len(array) > 1 {
				lang = array[len(array)-2]
			} else {
				lang = gfile.Name(array[0])
			}
			if m.data[lang] == nil {
				m.data[lang] = make(map[string]string)
			}
			content, err := m.options.FS.ReadFile(name)
			if err != nil {
				intlog.Errorf("load i18n file '%s' failed: %v", name, err)
				return nil
			}
			if j, err := gjson.LoadContent(content); err == nil {
				for k, v := range j.Map() {
					m.data[lang][k] = gconv.String(v)
				}
			} else {
				intlog.Errorf("load i18n file '%s' failed: %v", name, err)
			}
			return nil
		})
	} else if gres.Contains(m.options.Path) {
		files := gres.ScanDirFile(m.options.Path, "*.*", true)
		if len(files) > 0 {
			var (
				path  string
				name  string
				lang  string
				array []string
			)
			m.data = make(map[string]map[string]string)
			for _, file := range files {
				name = file.Name()
				path = name[len(m.options.Path)+1:]
				array = strings.Split(path, "/")
				if len(array) > 1 {
					lang = array[0]
				} else {
					lang = gfile.Name(array[0])
				}
				if m.data[lang] == nil {
					m.data[lang] = make(map[string]string)
				}
				if j, err := gjson.LoadContent(file.Content()); err == nil {
					for k, v := range j.Map() {
						m.data[lang][k] = gconv.String(v)
					}
				} else {
					intlog.Errorf("load i18n file '%s' failed: %v", name, err)
				}
			}
		}
	} else if m.options.Path != "" {
		files, _ := gfile.ScanDirFile(m.options.Path, "*.*", true)
		if len(files) == 0 {
			//intlog.Printf(
			//	"no i18n files found in configured directory: %s",
			//	m.options.Path,
			//)
			return
		}
		var (
			path  string
			lang  string
			array []string
		)
		m.data = make(map[string]map[string]string)
		for _, file := range files {
			path = file[len(m.options.Path)+1:]
			array = strings.Split(path, gfile.Separator)
			if len(array) > 1 {
				lang = array[0]
			} else {
				lang = gfile.Name(array[0])
			}
			if m.data[lang] == nil {
				m.data[lang] = make(map[string]string)
			}
			if j, err := gjson.LoadContent(gfile.GetBytes(file)); err == nil {
				for k, v := range j.Map() {
					m.data[lang][k] = gconv.String(v)
				}
				intlog.Printf("load i18n file '%s' success, lang: '%s'", file, lang)
			} else {
				intlog.Errorf("load i18n file '%s' failed: %v", file, err)
			}
		}
		// Monitor changes of i18n files for hot reload feature.
		cb, errCb := gfsnotify.Add(m.options.Path, func(event *gfsnotify.Event) {
			// Any changes of i18n files, clear the data.
			m.mu.Lock()
			m.data = nil
			intlog.Printf("i18n gfsnotify event: %+v, path: '%s'\r\n", event, m.options.Path)
			m.mu.Unlock()
			gfsnotify.Exit()
		})
		intlog.Printf("i18n add gfsnotify '%v' finish, errCb: '%v'", cb, errCb)
	}
}
