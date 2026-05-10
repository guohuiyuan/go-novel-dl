package site

import "strings"

type SiteMetadata struct {
	Title string
	Tags  []string
}

var siteMetadata = map[string]SiteMetadata{
	"aaatxt": {
		Title: "3A电子书",
		Tags:  []string{"简体中文", "转载站", "成人向", "NSFW"},
	},
	"alicesw": {
		Title: "爱丽丝书屋",
		Tags:  []string{"简体中文", "转载站", "成人向", "NSFW"},
	},
	"alphapolis": {
		Title: "アルファポリス",
		Tags:  []string{"日文", "原创", "免费", "轻小说"},
	},
	"akatsuki_novels": {
		Title: "暁",
		Tags:  []string{"日文", "原创", "免费", "同人二创"},
	},
	"biquge345": {
		Title: "笔趣阁",
		Tags:  []string{"简体中文", "转载站", "笔趣阁"},
	},
	"biquge5": {
		Title: "笔趣阁",
		Tags:  []string{"简体中文", "转载站", "笔趣阁"},
	},
	"ciweimao": {
		Title: "刺猬猫",
		Tags:  []string{"简体中文", "二次元", "同人二创", "原创"},
	},
	"ciyuanji": {
		Title: "次元姬",
		Tags:  []string{"简体中文", "二次元", "轻小说", "原创"},
	},
	"esjzone": {
		Title: "ESJ Zone",
		Tags:  []string{"简体中文", "轻小说", "转载站", "翻译", "NSFW"},
	},
	"faloo": {
		Title: "飞卢小说网",
		Tags:  []string{"简体中文", "原创", "男性向"},
	},
	"fanqienovel": {
		Title: "番茄小说网",
		Tags:  []string{"简体中文", "原创", "免费", "男性向", "女性向", "抖音"},
	},
	"fsshu": {
		Title: "笔趣阁",
		Tags:  []string{"简体中文", "转载站", "笔趣阁"},
	},
	"hongxiuzhao": {
		Title: "红袖招",
		Tags:  []string{"简体中文", "成人向", "NSFW"},
	},
	"ixdzs8": {
		Title: "爱下电子书",
		Tags:  []string{"简体中文", "转载站"},
	},
	"kadokado": {
		Title: "KadoKado",
		Tags:  []string{"繁体中文", "轻小说", "原创", "成人向", "NSFW"},
	},
	"linovelib": {
		Title: "哔哩轻小说",
		Tags:  []string{"简体中文", "轻小说", "转载站"},
	},
	"haiwaishubao": {
		Title: "海外书包",
		Tags:  []string{"简体中文", "转载站", "成人向", "NSFW"},
	},
	"n17k": {
		Title: "17K小说网",
		Tags:  []string{"简体中文", "原创", "男性向", "女性向"},
	},
	"n23qb": {
		Title: "铅笔小说",
		Tags:  []string{"简体中文", "转载站", "轻小说", "网络小说"},
	},
	"yodu": {
		Title: "有度中文网",
		Tags:  []string{"简体中文", "转载站", "网络小说"},
	},
	"qbtr": {
		Title: "全本同人小说",
		Tags:  []string{"简体中文", "转载站", "同人二创"},
	},
	"n69shuba": {
		Title: "69书吧",
		Tags:  []string{"简体中文", "转载站"},
	},
	"n8novel": {
		Title: "无限轻小说",
		Tags:  []string{"繁体中文", "轻小说", "转载站"},
	},
	"mjyhb": {
		Title: "三五中文",
		Tags:  []string{"简体中文", "转载站", "成人向", "NSFW"},
	},
	"novalpie": {
		Title: "노벨피아",
		Tags:  []string{"韩文", "原创", "免费", "轻小说", "成人向", "NSFW"},
	},
	"novelpia": {
		Title: "ノベルピア",
		Tags:  []string{"日文", "原创", "免费", "轻小说", "成人向", "NSFW"},
	},
	"ruochu": {
		Title: "若初文学网",
		Tags:  []string{"简体中文", "原创", "女性向"},
	},
	"sfacg": {
		Title: "SF轻小说",
		Tags:  []string{"简体中文", "轻小说", "原创"},
	},
	"syosetu": {
		Title: "小説家になろう",
		Tags:  []string{"日文", "原创", "免费", "轻小说"},
	},
	"syosetu18": {
		Title: "小説家になろう 18禁",
		Tags:  []string{"日文", "原创", "免费", "成人向", "NSFW"},
	},
	"syosetu_org": {
		Title: "ハーメルン",
		Tags:  []string{"日文", "原创", "免费", "同人二创", "转载站"},
	},
	"shuhaige": {
		Title: "书海阁小说网",
		Tags:  []string{"简体中文", "转载站", "笔趣阁"},
	},
	"wenku8": {
		Title: "轻小说文库",
		Tags:  []string{"简体中文", "轻小说", "转载站"},
	},
	"yibige": {
		Title: "一笔阁",
		Tags:  []string{"简体中文", "繁体中文", "转载站", "笔趣阁"},
	},
	"tongrenshe": {
		Title: "同人社",
		Tags:  []string{"同人二创", "简体中文", "转载站"},
	},
	"tianyabooks": {
		Title: "天涯书库",
		Tags:  []string{"简体中文", "经典图书", "转载站"},
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
