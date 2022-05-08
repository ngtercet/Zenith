package i18n

import (
	log "github.com/sirupsen/logrus"
	"zenith/pkg/jsonutil"

	"github.com/leonelquinteros/gotext"
	"github.com/tidwall/gjson"
)

func Tran(field string, json *gjson.Result, mo *gotext.Mo) string {
	if raw, has := jsonutil.GetField(field, json); !has {
		return ""
	} else {
		var res string
		if jsonutil.IsString(raw) {
			res = mo.Get(raw.String())
		} else {
			var str, strSp, ctxt, strPl string
			var has bool
			ctxt, _ = jsonutil.GetString("ctxt", raw, "")
			if str, has = jsonutil.GetString("str", raw, ""); has {
				strPl, _ = jsonutil.GetString("str_pl", raw, "")
			} else if strSp, has = jsonutil.GetString("str_sp", raw, ""); has {
				str = strSp
				strPl = strSp
			} else {
				log.Debugf("name format is invalid: %s", raw.String())
			}

			if ctxt != "" {
				if strPl != "" {
					res = mo.GetNC(str, strPl, 1, ctxt)
				} else {
					res = mo.GetC(str, ctxt)
				}
			} else {
				if strPl != "" {
					res = mo.GetN(str, strPl, 1)
				} else {
					res = mo.Get(str)
				}
			}
		}
		return res
	}
}

func TranString(raw string, mo *gotext.Mo) string {
	if mo == nil {
		return raw
	}
	return mo.Get(raw)
}

func TranCustom(raw string, po *gotext.Po) string {
	if po == nil {
		return raw
	}
	return po.Get(raw)
}
