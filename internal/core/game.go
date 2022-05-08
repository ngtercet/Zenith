package core

import (
	"fmt"
	"strconv"
	"strings"
	"zenith/internal/loader"
	"zenith/pkg/fileutil"
	"zenith/pkg/jsonutil"

	"github.com/leonelquinteros/gotext"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

type Game struct {
	Commit    string
	UpdateAt  string
	Mods      map[string]*Mod // id -> mod
	ModPath   string
	LangPacks map[string]LangPack
	Indexer   Indexer
}

type LangPack struct {
	Lang string
	Mo   *gotext.Mo
	Po   *gotext.Po
}

func (game *Game) Load(targets map[string]bool) {
	if err := game.preLoad(); err != nil {
		log.Fatal(err)
	}

	for _, mod := range game.Mods {
		if len(targets) > 0 {
			if _, ok := targets[mod.Id]; ok && !mod.Loaded {
				game.doLoad(mod)
			}
		} else {
			game.doLoad(mod)
		}
	}

	game.postLoad()
}

func (game *Game) preLoad() error {
	langs := []string{"en_US", "zh_CN"}
	for _, lang := range langs {
		game.LangPacks[lang] = LangPack{
			Lang: lang,
			Mo:   loader.LoadMo(lang),
			Po:   loader.LoadPo(lang),
		}
	}

	if _, dirs, err := fileutil.Ls(game.ModPath); err != nil {
		return err
	} else {
		for _, dir := range dirs {
			if files, _, err := fileutil.Ls(dir); err != nil {
				return err
			} else {
				for _, file := range files {
					if strings.HasSuffix(file, "modinfo.json") {
						res := loader.LoadJsonFromFile(file)
						modInfo := *res[0]
						id := modInfo.Get("id").String()

						var path string
						jsonPath := modInfo.Get("path")
						if !jsonPath.Exists() {
							path = dir
						} else {
							path = dir + "/" + jsonPath.String()
						}
						var dependencies []string
						jsonDp := modInfo.Get("dependencies")
						if jsonDp.Exists() {
							for _, dp := range jsonDp.Array() {
								dependencies = append(dependencies, dp.String())
							}
						}

						mod := &Mod{
							Id:           id,
							Name:         modInfo.Get("name").String(),
							Description:  modInfo.Get("description").String(),
							Path:         path,
							Dependencies: dependencies,
							TempData:     make(map[string]map[string]*gjson.Result),
							Loaded:       false,
						}

						game.Mods[id] = mod
					}
				}
			}
		}
	}

	game.Indexer = NewMemIndexer(game.Mods, game.LangPacks)

	return nil
}

func (game *Game) doLoad(mod *Mod) {
	if mod.Loaded {
		return
	}

	dependencies := mod.Dependencies
	for _, dependency := range dependencies {
		m := game.Mods[dependency]
		if m != nil {
			game.doLoad(m)
		} else {
			log.Warnf("%v's dependency: %v is not found.", mod.Name, dependency)
		}
	}

	path := mod.Path
	dirJsons := loader.LoadJsonFromPaths(path)

	for _, jsons := range dirJsons {
		temp := make([]*gjson.Result, 0)
		for _, json := range jsons {
			id := getId(json)
			tp := getType(json)
			if id == "" {
				log.Debugf("id not found, tp: %s", tp)
				continue
			}

			if !isInAllowList(json) {
				continue
			}

			if _, has := mod.TempData[tp]; !has {
				mod.TempData[tp] = make(map[string]*gjson.Result)
			}
			mod.TempData[tp][id] = json
			temp = append(temp, json)
		}

		for _, json := range temp {
			if loader.NeedInherit(json) {
				if game.inherit(mod, json) {
					mod.CreateIndex(game.Indexer, json, game.LangPacks)
				} else {
					log.Error("inherit failed")
				}
			} else {
				mod.CreateIndex(game.Indexer, json, game.LangPacks)
			}
		}
	}

	mod.Loaded = true
}

func (game *Game) postLoad() {
	game.Indexer.PrintItemNum()
}

func (game *Game) inherit(mod *Mod, json *gjson.Result) bool {
	cf := json.Get("copy-from")
	if !cf.Exists() {
		return false
	}
	parId := cf.String()
	tp := getType(json)
	id := getId(json)

	if pars := mod.TempData[tp]; pars != nil {
		if par, has := pars[parId]; has && par != json {
			if loader.NeedInherit(par) && !game.inherit(mod, par) {
				log.Warnf("inherit failed, id: %v, par id: %v", id, parId)
				return false
			}

			jsonStr := par.String()
			json.ForEach(func(k, v gjson.Result) bool {
				field := k.String()
				switch field {
				case "relative":
					inheritRelative(&jsonStr, par, json, "relative")
				case "proportional":
					inheritProportional(&jsonStr, par, json, "proportional")
				case "extend":
					v.ForEach(func(ck, cv gjson.Result) bool {
						vInPar := par.Get(ck.String())
						var res []interface{}
						if vInPar.Exists() {
							for _, elem := range vInPar.Array() {
								res = append(res, elem.Value())
							}
						}
						for _, elem := range cv.Array() {
							res = append(res, elem.Value())
						}

						jsonutil.Set(&jsonStr, ck.String(), res)
						return true
					})
				case "delete":
					v.ForEach(func(ck, cv gjson.Result) bool {
						vInCur := gjson.Get(jsonStr, ck.String())
						if vInCur.Exists() {
							var res []string

							// FIXME fully support delete
							if !vInCur.IsArray() {
								id := json.Get("id").String()
								log.Debugf("delete field is not supported, id: %v", id)
								return true
							}

							for _, elem := range vInCur.Array() {
								flag := false
								for _, cvElem := range cv.Array() {
									if elem.String() == cvElem.String() {
										flag = true
									}
									break
								}
								if !flag {
									res = append(res, elem.String())
								}
							}
							jsonutil.Set(&jsonStr, ck.String(), res)
						}

						// we assume that delete is done from self
						if par.Get(ck.String()).Exists() && !vInCur.Exists() {
							log.Debugf("%s field delete is abnormal", json)
						}
						return true
					})

				case "copy-from":
					// discard
				default:
					jsonutil.Set(&jsonStr, k.String(), v.Value())
				}
				return true
			})
			*json = gjson.Parse(jsonStr)
			return true
		}
	}

	for _, dp := range mod.Dependencies {
		dpMod := game.Mods[dp]
		if game.inherit(dpMod, json) {
			return true
		}
	}

	log.Warnf("inherit failed, id: %v, par id: %v", id, parId)
	return false
}

func getId(json *gjson.Result) string {
	var id string
	var has bool
	if id, has = jsonutil.GetString("id", json, ""); has {
		return id
	}
	if id, has = jsonutil.GetString("abstract", json, ""); has {
		return id
	}
	return ""
}

func getType(json *gjson.Result) string {
	tp, _ := jsonutil.GetString("type", json, "")
	return tp
}

func inheritRelative(jsonStr *string, par *gjson.Result, json *gjson.Result, path string) {
	json.Get(path).ForEach(func(k, v gjson.Result) bool {

		if v.IsObject() {
			inheritRelative(jsonStr, par, json, path+"."+k.String())
		} else if v.Type == gjson.Number {
			fromPath := path + "." + k.String()
			toPath := strings.Split(fromPath, "relative.")[1]
			parVal := par.Get(toPath)
			if parVal.Exists() {
				jsonutil.Set(jsonStr, toPath, parVal.Int()+v.Int())
			}
		}
		return true
	})
}

func inheritProportional(jsonStr *string, par *gjson.Result, json *gjson.Result, path string) {
	json.Get(path).ForEach(func(k, v gjson.Result) bool {

		if v.IsObject() {
			inheritProportional(jsonStr, par, json, path+"."+k.String())
		} else if v.Type == gjson.Number {
			fromPath := path + "." + k.String()

			toPath := strings.Split(fromPath, "proportional.")[1]

			parVal := par.Get(toPath)

			if parVal.Exists() {
				val, _ := strconv.ParseFloat(fmt.Sprintf("%.2f", parVal.Float()*v.Float()), 64)
				jsonutil.Set(jsonStr, toPath, val)
			}
		}
		return true
	})
}

func isInAllowList(json *gjson.Result) bool {
	type_, _ := jsonutil.GetString("type", json, "")

	// TODO add new type, step 1
	allowList := map[string]bool{
		"MONSTER":        true,
		"monster_attack": true,
		"effect_type":    true,
	}
	return allowList[type_]
}
