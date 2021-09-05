package core

import (
	"fmt"
	"strconv"
	"strings"
	"zenith/internal/data"
	"zenith/internal/loader"
	"zenith/pkg/fileutil"
	"zenith/pkg/jsonutil"

	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

type Game struct {
	Version string
	Mods    map[string]*data.Mod
	ModPath string
}

func (game *Game) LoadMod(targets map[string]bool) {

	if err := game.preLoadMod(); err != nil {
		log.Fatal(err)
	}

	for _, mod := range game.Mods {
		if len(targets) > 0 {
			if _, ok := targets[mod.ID]; ok && !mod.Loaded {
				game.doLoadMod(mod)
			}
		} else {
			game.doLoadMod(mod)
		}
	}

	game.postLoadMod()
}

func (game *Game) preLoadMod() error {

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

						mod := &data.Mod{
							ID:           id,
							Name:         modInfo.Get("name").String(),
							Description:  modInfo.Get("description").String(),
							Path:         path,
							Dependencies: dependencies,
							Data:         make(map[string][]string),
							TempData:     make(map[string][]*gjson.Result),
							Loaded:       false,
						}

						game.Mods[id] = mod
					}
				}
			}
		}
	}

	return nil
}

func (game *Game) postLoadMod() {
	// TODO
	log.Info(game.Mods["dda"].TempData["mon_zombie_soldier"])
	log.Info(game.Mods["dda"].TempData["mon_zombie_soldier_acid_2"])
}

func (game *Game) doLoadMod(mod *data.Mod) {
	dependencies := mod.Dependencies
	for _, dependency := range dependencies {
		m := game.Mods[dependency]
		if !m.Loaded {
			game.doLoadMod(m)
		}
	}
	path := mod.Path
	jsons := loader.LoadJsonFromPaths(path)
	game.processModData(mod, jsons)

	mod.Loaded = true
	log.Printf("[MOD]: %s is loaded", mod.ID)
}

func (game *Game) processModData(mod *data.Mod, jsons []*gjson.Result) {
	for _, json := range jsons {
		id := getId(json)
		if id == "" {
			log.Debug("id not found, json: %s", json.String())
			continue
		}

		tar := mod.TempData[id]
		mod.TempData[id] = append(tar, json)
	}

	for _, tempJsonList := range mod.TempData {
		for _, tempJson := range tempJsonList {
			if needInherit(tempJson) {
				game.inherit(mod, tempJson)
			}
		}
	}
}

func needInherit(json *gjson.Result) bool {
	return json.Get("copy-from").Exists()
}

func isAbstract(json *gjson.Result) bool {
	return json.Get("abstract").Exists()
}

func getId(json *gjson.Result) string {
	id := json.Get("id")
	if id.Exists() {
		return id.String()
	}
	ab := json.Get("abstract")
	if ab.Exists() {
		return ab.String()
	}
	return ""
}

func (game *Game) inherit(mod *data.Mod, json *gjson.Result) bool {
	cf := json.Get("copy-from")
	if !cf.Exists() {
		return false
	}
	parId := cf.String()

	flag := false
	if pars := mod.TempData[parId]; pars != nil {
		for _, par := range pars {
			if par.Get("type").String() == json.Get("type").String() {
				if needInherit(par) {
					game.inherit(mod, par)
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
							var res []string
							if vInPar.Exists() {
								for _, elem := range vInPar.Array() {
									res = append(res, elem.String())
								}
							}
							for _, elem := range cv.Array() {
								res = append(res, elem.String())
							}

							jsonutil.Set(&jsonStr, ck.String(), res)
							return true
						})
					case "delete":
						v.ForEach(func(ck, cv gjson.Result) bool {
							vInCur := gjson.Get(jsonStr, ck.String())
							if vInCur.Exists() {
								var res []string
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
						jsonutil.Set(&jsonStr, k.String(), v.String())
					}

					return true
				})

				*json = gjson.Parse(jsonStr)
				flag = true

				break
			}
		}

	}

	if !flag {
		for _, dp := range mod.Dependencies {
			dpMod := game.Mods[dp]
			if game.inherit(dpMod, json) {
				break
			}
		}
	}

	return true
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
