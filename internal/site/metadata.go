package site

import "strings"

type SiteMetadata struct {
	Title string
	Tags  []string
}

var siteMetadata = map[string]SiteMetadata{
	"alicesw": {
		Title: "爱丽丝书屋",
		Tags:  []string{"简体中文", "转载站", "成人向", "NSFW"},
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
	"linovelib": {
		Title: "哔哩轻小说",
		Tags:  []string{"简体中文", "轻小说", "转载站"},
	},
	"n17k": {
		Title: "17K小说网",
		Tags:  []string{"简体中文", "原创", "男性向", "女性向"},
	},
	"n23qb": {
		Title: "铅笔小说",
		Tags:  []string{"简体中文", "转载站", "轻小说", "网络小说"},
	},
	"n69shuba": {
		Title: "69书吧",
		Tags:  []string{"简体中文", "转载站"},
	},
	"n8novel": {
		Title: "无限轻小说",
		Tags:  []string{"繁体中文", "轻小说", "转载站"},
	},
	"novalpie": {
		Title: "ノベルピア",
		Tags:  []string{"日文", "原创", "免费", "轻小说", "成人向", "NSFW"},
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
