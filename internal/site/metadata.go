package site

import "strings"

type SiteMetadata struct {
	Title string
	Tags  []string
}

var siteMetadata = map[string]SiteMetadata{
	"aaatxt": {
		Title: "3A电子书",
		Tags:  []string{"网络小说", "成人向"},
	},
	"alicesw": {
		Title: "爱丽丝书屋",
		Tags:  []string{"轻小说", "成人向"},
	},
	"alphapolis": {
		Title: "アルファポリス",
		Tags:  []string{"日文", "原创", "轻小说"},
	},
	"akatsuki_novels": {
		Title: "暁",
		Tags:  []string{"日文", "原创", "同人二创"},
	},
	"biquge345": {
		Title: "笔趣阁",
		Tags:  []string{"网络小说", "笔趣阁"},
	},
	"biquge5": {
		Title: "笔趣阁",
		Tags:  []string{"网络小说", "笔趣阁"},
	},
	"ciweimao": {
		Title: "刺猬猫",
		Tags:  []string{"原创", "二次元", "同人二创"},
	},
	"ciyuanji": {
		Title: "次元姬",
		Tags:  []string{"原创", "二次元", "轻小说"},
	},
	"czbooks": {
		Title: "小说狂人",
		Tags:  []string{"繁体中文", "成人向"},
	},
	"esjzone": {
		Title: "ESJ Zone",
		Tags:  []string{"轻小说", "翻译", "成人向"},
	},
	"faloo": {
		Title: "飞卢小说网",
		Tags:  []string{"原创", "男性向", "飞卢"},
	},
	"fanqienovel": {
		Title: "番茄小说网",
		Tags:  []string{"原创"},
	},
	"shuhuangdian": {
		Title: "书荒殿",
		Tags:  []string{"网络小说", "起点", "番茄小说"},
	},
	"qidian": {
		Title: "起点中文网",
		Tags:  []string{"原创", "阅文"},
	},
	"qqreader": {
		Title: "QQ阅读",
		Tags:  []string{"原创", "阅文"},
	},
	"weread": {
		Title: "微信读书",
		Tags:  []string{"出版物", "正版"},
	},
	"fsshu": {
		Title: "笔趣阁",
		Tags:  []string{"网络小说", "笔趣阁"},
	},
	"hongxiuzhao": {
		Title: "红袖招",
		Tags:  []string{"网络小说", "成人向"},
	},
	"ixdzs8": {
		Title: "爱下电子书",
		Tags:  []string{"网络小说"},
	},
	"xiguashuwu": {
		Title: "西瓜书屋",
		Tags:  []string{"网络小说", "成人向"},
	},
	"kadokado": {
		Title: "KadoKado",
		Tags:  []string{"繁体中文", "轻小说", "成人向"},
	},
	"linovelib": {
		Title: "哔哩轻小说",
		Tags:  []string{"轻小说"},
	},
	"buguxs": {
		Title: "布谷小说网",
		Tags:  []string{"网络小说"},
	},
	"linovel": {
		Title: "轻之文库",
		Tags:  []string{"原创", "轻小说"},
	},
	"lnovel": {
		Title: "轻小说百科",
		Tags:  []string{"繁体中文", "轻小说", "网络小说"},
	},
	"ltxswu": {
		Title: "联天书屋",
		Tags:  []string{"网络小说", "成人向"},
	},
	"haiwaishubao": {
		Title: "海外书包",
		Tags:  []string{"网络小说", "成人向"},
	},
	"n17k": {
		Title: "17K小说网",
		Tags:  []string{"原创"},
	},
	"n23qb": {
		Title: "铅笔小说",
		Tags:  []string{"轻小说", "网络小说"},
	},
	"n37yq": {
		Title: "三七轻小说",
		Tags:  []string{"轻小说"},
	},
	"yodu": {
		Title: "有度中文网",
		Tags:  []string{"网络小说"},
	},
	"qbtr": {
		Title: "全本同人小说",
		Tags:  []string{"网络小说", "同人二创"},
	},
	"n69shuba": {
		Title: "69书吧",
		Tags:  []string{"网络小说"},
	},
	"n8novel": {
		Title: "无限轻小说",
		Tags:  []string{"繁体中文", "轻小说"},
	},
	"mjyhb": {
		Title: "三五中文",
		Tags:  []string{"网络小说", "成人向"},
	},
	"novalpie": {
		Title: "Novalpie",
		Tags:  []string{"韩文", "原创", "成人向"},
	},
	"novelpia": {
		Title: "Novelpia",
		Tags:  []string{"韩文", "轻小说", "成人向"},
	},
	"ruochu": {
		Title: "若初文学网",
		Tags:  []string{"原创", "女性向"},
	},
	"sfacg": {
		Title: "SF轻小说",
		Tags:  []string{"原创", "轻小说", "二次元"},
	},
	"shencou": {
		Title: "神凑轻小说",
		Tags:  []string{"轻小说"},
	},
	"syosetu": {
		Title: "小説家になろう",
		Tags:  []string{"日文", "原创", "轻小说"},
	},
	"syosetu18": {
		Title: "小説家になろう 18禁",
		Tags:  []string{"日文", "原创", "成人向"},
	},
	"syosetu_org": {
		Title: "ハーメルン",
		Tags:  []string{"日文", "同人二创"},
	},
	"shuhaige": {
		Title: "书海阁小说网",
		Tags:  []string{"网络小说", "笔趣阁"},
	},
	"wenku8": {
		Title: "轻小说文库",
		Tags:  []string{"轻小说"},
	},
	"yibige": {
		Title: "一笔阁",
		Tags:  []string{"繁体中文", "网络小说", "笔趣阁"},
	},
	"tongrenshe": {
		Title: "同人社",
		Tags:  []string{"网络小说", "同人二创"},
	},
	"tianyabooks": {
		Title: "天涯书库",
		Tags:  []string{"经典图书"},
	},
}

func descriptorMetadata(key string) SiteMetadata {
	key = strings.ToLower(strings.TrimSpace(key))
	item, ok := siteMetadata[key]
	if !ok {
		return SiteMetadata{}
	}
	item.Tags = append([]string(nil), item.Tags...)
	return item
}
