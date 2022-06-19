package controller

import (
	"github.com/ego008/goutils/json"
	"github.com/ego008/sdb"
	"github.com/rs/xid"
	"github.com/valyala/fasthttp"
	"goyoubbs/lib/qqOAuth"
	"goyoubbs/model"
	"goyoubbs/util"
	"strconv"
	"strings"
	"time"
)

func (h *BaseHandler) QQOauthHandler(ctx *fasthttp.RequestCtx) {
	scf := h.App.Cf.Site
	qq, err := qqOAuth.NewQQOAuth(scf.QQClientID, scf.QQClientSecret, scf.MainDomain+"/oauth/qq/callback")
	if err != nil {
		_, _ = ctx.WriteString(err.Error())
		return
	}
	// qqOAuth.Logging = true

	now := time.Now().UTC().Unix()
	qqUrlState := strconv.FormatInt(now, 10)[6:]

	urlStr, err := qq.GetAuthorizationURL(qqUrlState)
	if err != nil {
		_, _ = ctx.WriteString(err.Error())
		return
	}

	_ = h.SetCookie(ctx, "QQUrlState", qqUrlState, 1)
	ctx.Redirect(urlStr, fasthttp.StatusSeeOther)
}

func (h *BaseHandler) QQOauthCallback(ctx *fasthttp.RequestCtx) {
	qqUrlState := h.GetCookie(ctx, "QQUrlState")
	if len(qqUrlState) == 0 {
		_, _ = ctx.WriteString(`qqUrlState cookie missed`)
		return
	}

	scf := h.App.Cf.Site
	qq, err := qqOAuth.NewQQOAuth(scf.QQClientID, scf.QQClientSecret, scf.MainDomain+"/oauth/qq/callback")
	if err != nil {
		_, _ = ctx.WriteString(err.Error())
		return
	}
	// qqOAuth.Logging = true

	code := sdb.B2s(ctx.FormValue("code"))
	if code == "" {
		_, _ = ctx.WriteString("Invalid code")
		return
	}

	state := sdb.B2s(ctx.FormValue("state"))
	if state != qqUrlState {
		_, _ = ctx.WriteString("Invalid state")
		return
	}

	token, err := qq.GetAccessToken(code)
	if err != nil {
		_, _ = ctx.WriteString(err.Error())
		return
	}

	openid, err := qq.GetOpenID(token.AccessToken)
	if err != nil {
		_, _ = ctx.WriteString(err.Error())
		return
	}

	timeStamp := uint64(time.Now().UTC().Unix())
	next := h.GetCookie(ctx, "next")

	db := h.App.Db
	authorKey := "qq:" + openid.OpenID
	rs := db.Hget("oauth2user", []byte(authorKey))
	if rs.OK() {
		// login
		obj := model.AuthInfo{}
		_ = json.Unmarshal(rs.Data[0], &obj)

		if obj.Uid > 0 {
			// 已绑定用户名则直接登录
			uObj, _ := model.UserGetById(db, obj.Uid)
			if uObj.ID == 0 {
				_, _ = ctx.WriteString("uid not found")
				return
			}
			sessionId := xid.New().String()
			uObj.LastLoginTime = timeStamp
			uObj.Session = sessionId
			jb, _ := json.Marshal(uObj)
			_ = db.Hset(model.UserTbName, sdb.I2b(uObj.ID), jb)
			_ = h.SetCookie(ctx, "SessionID", strconv.FormatUint(uObj.ID, 10)+":"+sessionId, 365)

			if len(next) > 0 {
				h.DelCookie(ctx, "next")
				ctx.Redirect(scf.MainDomain+next, fasthttp.StatusSeeOther)
				return
			}
			ctx.Redirect(scf.MainDomain+"/", fasthttp.StatusSeeOther)
			return
		}
	}

	jb, _ := json.Marshal(model.AuthInfo{Openid: openid.OpenID})
	_ = db.Hset("oauth2user", sdb.S2b(authorKey), jb)

	// 绑定用户名，跳到注册页面，填写默认登录名

	if scf.CloseReg {
		_, _ = ctx.WriteString(`stop to new register`)
		return
	}

	// 保存 openid ，以便在 注册 时取出可用登录名及注册成功后自动获取头像
	_ = h.SetCookie(ctx, "openid", authorKey, 1)

	// 获取用户名和头像
	profile, err := qq.GetUserInfo(token.AccessToken, openid.OpenID)
	if err == nil {
		if profile.Ret == 0 {
			name := util.RemoveCharacter(profile.Nickname)
			name = strings.TrimSpace(strings.Replace(name, " ", "", -1))
			if len(name) > 0 {
				nameLow := strings.ToLower(name)
				if db.Hget("user_name2uid", []byte(nameLow)).OK() {
					name = ""
				}
			}

			jb, _ := json.Marshal(model.AuthProfileInfo{
				LoginBy: "qq",
				OpenId:  openid.OpenID,
				Name:    name,
				Avatar:  profile.Avatar,
				Agent:   sdb.B2s(ctx.UserAgent()),
			})
			_ = db.Hset("oauth_tmp_info", sdb.S2b(authorKey), jb)
		}
	}

	ctx.Redirect(scf.MainDomain+"/register", fasthttp.StatusSeeOther)
}
