package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"golang.org/x/net/html"
)

const (
	AvitoURL      = "жесткий диск 500гб"
	BotToken      = ""
	ChatID        = 
	MaxPrice      = 500
	CheckEvery    = 5 * time.Minute
	NoResultsTime = 30 * time.Minute
	UseProxy      = true
)

type Proxy struct {
	IP   string `json:"ip"`
	Port string `json:"port"`
}

type Ad struct {
	ID    string
	Title string
	Price string
	URL   string
	Img   string
}

var (
	lastFoundTime time.Time
	lastCheckTime time.Time
	botInstance   *tgbotapi.BotAPI
	totalChecks   int
	totalFound    int
	currentProxy  string
	proxyList     []string
	proxyIndex    int
	targetSizes   = []string{"500gb", "1tb", "500гб", "1 тб", "1тб", "500 гб", "500Гб", "1000gb", "1Тб", "500gb", "0.5tb", "500 gb", "0.5 tb"}
	userAgents    = []string{
		"Mozilla/5.0 (Linux; Android 7.0; SM-G930VC Build/NRD90M; wv)",
		"Chrome/70.0.3538.77 Safari/537.36",
		"Opera/9.68 (X11; Linux i686; en-US) Presto/2.9.344 Version/11.00",
		"Mozilla/5.0 (compatible; MSIE 10.0; Windows 95; Trident/5.1)",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_7_6) AppleWebKit/5342 (KHTML, like Gecko) Chrome/37.0.896.0 Mobile Safari/5342",
	}
)

func main() {
	fmt.Println("Starting Avito Parser with Proxy Rotation...")

	// Initialize Telegram bot
	bot, err := tgbotapi.NewBotAPI(BotToken)
	if err != nil {
		log.Panic("Telegram bot error:", err)
	}
	botInstance = bot

	_, err = bot.Request(tgbotapi.DeleteWebhookConfig{})
	if err != nil {
		log.Printf("Error removing webhook: %v", err)
	}

	lastFoundTime = time.Now()
	lastCheckTime = time.Now()

	// Initialize proxy list
	proxyList = []string{
		"http://83.217.23.34:8090",
		"http://185.221.160.17:80",
		"http://185.148.107.58:80",
	}
	currentProxy = proxyList[0]

	sendMessage(fmt.Sprintf(
		"🔍 Бот начал работу\nИщу диски 500GB/1TB до %d руб\nПроверка каждые %v\nИспользуется прокси: %s",
		MaxPrice, CheckEvery, currentProxy,
	))

	go commandHandler()
	go checkNoResults()

	for {
		checkAvito()
		time.Sleep(CheckEvery)
	}
}

func getProxy() (string, error) {
	if len(proxyList) == 0 {
		return "", fmt.Errorf("no proxies available")
	}

	// Rotate proxies
	proxyIndex = (proxyIndex + 1) % len(proxyList)
	currentProxy = proxyList[proxyIndex]
	return currentProxy, nil
}

func createHTTPClient() *http.Client {
	if !UseProxy {
		return &http.Client{Timeout: 30 * time.Second}
	}

	proxy, err := getProxy()
	if err != nil {
		log.Printf("Proxy error: %v, continuing without proxy", err)
		return &http.Client{Timeout: 30 * time.Second}
	}

	proxyURL, err := url.Parse(proxy)
	if err != nil {
		log.Printf("Invalid proxy URL: %v", err)
		return &http.Client{Timeout: 30 * time.Second}
	}

	return &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
		Timeout: 30 * time.Second,
	}
}

func sendRequest(query string) (*http.Response, error) {
	client := createHTTPClient()

	encodedQuery := url.QueryEscape(query)
	finalURL := "https://www.avito.ru/all?cd=1&q=" + encodedQuery

	req, err := http.NewRequest("GET", finalURL, nil)
	if err != nil {
		return nil, err
	}

	// Random user agent
	rand.Seed(time.Now().UnixNano())
	req.Header.Set("User-Agent", userAgents[rand.Intn(len(userAgents))])
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "ru-RU,ru;q=0.9,en-US;q=0.8,en;q=0.7")

	// Random delay
	time.Sleep(time.Duration(1+rand.Intn(3)) * time.Second)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		resp.Body.Close()
		log.Printf("Request failed with status: %s, body: %s", resp.Status, body)
		return nil, fmt.Errorf("bad request: %s", resp.Status)
	}

	return resp, nil
}

func sendMessage(text string) {
	msg := tgbotapi.NewMessage(ChatID, text)
	_, err := botInstance.Send(msg)
	if err != nil {
		log.Printf("Ошибка отправки сообщения: %v", err)
	}
}

func commandHandler() {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	retryDelay := 3 * time.Second
	maxRetries := 5

	for i := 0; i < maxRetries; i++ {
		updates := botInstance.GetUpdatesChan(u)

		for update := range updates {
			if update.Message == nil {
				continue
			}

			if update.Message.Text == "/info" {
				sendStatusInfo()
			}
		}

		log.Printf("Соединение прервано, повторная попытка %d/%d через %v", i+1, maxRetries, retryDelay)
		time.Sleep(retryDelay)
	}

	log.Println("Не удалось установить соединение после нескольких попыток")
}

func sendStatusInfo() {
	message := fmt.Sprintf(
		"🤖 Статус бота:\n\n"+
			"• Последняя проверка: %v назад\n"+
			"• Последняя находка: %v назад\n"+
			"• Всего проверок: %d\n"+
			"• Найдено объявлений: %d\n"+
			"• Текущий прокси: %s\n\n"+
			"Параметры поиска:\n"+
			"• Размеры: 500GB/1TB\n"+
			"• Макс. цена: %d руб",
		time.Since(lastCheckTime).Round(time.Minute),
		time.Since(lastFoundTime).Round(time.Minute),
		totalChecks,
		totalFound,
		currentProxy,
		MaxPrice,
	)

	sendMessage(message)
}

func checkNoResults() {
	for {
		if time.Since(lastFoundTime) > NoResultsTime {
			sendMessage(
				fmt.Sprintf("⚠️ За последние %v не найдено:\n\n- 500GB/1TB\n- Дешевле %d руб",
					NoResultsTime, MaxPrice))
			lastFoundTime = time.Now()
		}
		time.Sleep(5 * time.Minute)
	}
}

func checkAvito() {
	totalChecks++
	lastCheckTime = time.Now()
	foundAny := false

	resp, err := sendRequest(AvitoURL)
	if err != nil {
		log.Println("Request error:", err)
		return
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		log.Println("Parsing error:", err)
		return
	}

	doc.Find(".iva-item-content").Each(func(i int, s *goquery.Selection) {
		title := strings.ToLower(s.Find("h3").Text())
		priceStr, _ := s.Find("meta[itemprop=price]").Attr("content")
		link, _ := s.Find("a[itemprop=url]").Attr("href")
		link = "https://www.avito.ru" + link

		price := toInt(priceStr)
		if price <= MaxPrice && containsSize(title) {
			totalFound++
			size := detectSize(title)
			sendMessage(
				fmt.Sprintf("💾 %s\n💰 %d руб\n📏 %s\n🔗 %s",
					strings.Title(title), price, strings.ToUpper(size), link))
			lastFoundTime = time.Now()
			foundAny = true
		}
	})

	if foundAny {
		log.Println("Найдены подходящие объявления")
	}
}

func containsSize(title string) bool {
	for _, size := range targetSizes {
		if strings.Contains(title, size) {
			return true
		}
	}
	return false
}

func detectSize(title string) string {
	title = strings.ToLower(title)
	if strings.Contains(title, "1tb") || strings.Contains(title, "1000gb") ||
		strings.Contains(title, "1тб") || strings.Contains(title, "1 тб") {
		return "1TB"
	}
	return "500GB"
}

func toInt(s string) int {
	cleaned := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)

	num, err := strconv.Atoi(cleaned)
	if err != nil {
		return 0
	}
	return num
}

func getAdsList(avitoSearchURL string) ([]Ad, error) {
	htmlContent, err := getHTML(avitoSearchURL)
	if err != nil {
		return nil, err
	}

	doc, err := html.Parse(strings.NewReader(string(htmlContent))) // Исправлено здесь
	if err != nil {
		return nil, err
	}

	var ads []Ad
	var f func(*html.Node)
	f = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "article" { // Убедитесь, что это правильный тег
			// Извлечение данных об объявлении
			ad := Ad{
				ID:    "example_id", // Замените на реальное извлечение данных
				Title: "example_title",
				Price: "example_price",
				URL:   "example_url",
				Img:   "example_img",
			}
			ads = append(ads, ad)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(doc)

	return ads, nil
}

func getNewAds(newAds, oldAds []Ad) []Ad {
	var newUniqueAds []Ad
	adMap := make(map[string]struct{})
	for _, ad := range oldAds {
		adMap[ad.ID] = struct{}{}
	}
	for _, ad := range newAds {
		if _, exists := adMap[ad.ID]; !exists {
			newUniqueAds = append(newUniqueAds, ad)
		}
	}
	return newUniqueAds
}

func getHTML(url string) ([]byte, error) {
	client := createHTTPClient()

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User -Agent", userAgents[rand.Intn(len(userAgents))])

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch URL %s: %s", url, resp.Status)
	}

	return ioutil.ReadAll(resp.Body)
}
