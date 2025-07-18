package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

const (
	SpreadsheetID = "1Mu68FPwqR4U3gHgnuXtYAeQzgl6j2dXiOy9QzBqx-Vo" // <-- faqat ID, to‘liq URL emas
	SheetName     = "DALONG"
)

func usernameExists(srv *sheets.Service, username string) bool {
	readRange := SheetName + "!A1:Z1000"
	resp, err := srv.Spreadsheets.Values.Get(SpreadsheetID, readRange).Do()
	if err != nil {
		log.Printf("Error reading sheet for username check: %v", err)
		return false
	}

	if len(resp.Values) < 1 {
		return false
	}

	header := resp.Values[0]
	var usernameIdx int = -1
	for i, val := range header {
		if strings.TrimSpace(fmt.Sprintf("%v", val)) == "Username" {
			usernameIdx = i
			break
		}
	}
	if usernameIdx == -1 {
		return false
	}

	for _, row := range resp.Values[1:] {
		if len(row) > usernameIdx {
			val := fmt.Sprintf("%v", row[usernameIdx])
			if val == username {
				return true
			}
		}
	}
	return false
}

func main() {
	// Telegram botni ishga tushurish
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if botToken == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN environment variable is required")
	}

	bot, err := tgbotapi.NewBotAPI(botToken)
	if err != nil {
		log.Panic(err)
	}

	log.Printf("Authorized on account %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	// Google Sheets client
	ctx := context.Background()
	srv, err := sheets.NewService(ctx, option.WithCredentialsFile("credentials.json"))
	if err != nil {
		log.Fatalf("Unable to retrieve Sheets client: %v", err)
	}

	for update := range updates {
		// Kategoriyalarni oldindan olish
		categories, err := getCategories(srv)
		if err != nil {
			log.Fatalf("Kategoriya olishda xatolik: %v", err)
		}

		// Text message from user
		if update.Message != nil {
			username := update.Message.From.UserName

			if !usernameExists(srv, username) {
				msg := tgbotapi.NewMessage(update.Message.Chat.ID, "⛔ Siz ro'yxatdan o'tmagansiz.")
				bot.Send(msg)
				continue
			}

			msg := tgbotapi.NewMessage(update.Message.Chat.ID, "Mahsulot Kategoriyasini tanlang:")
			msg.ReplyMarkup = categoryKeyboard(categories)
			bot.Send(msg)
		}

		// User clicked a button
		if update.CallbackQuery != nil {
			username := update.CallbackQuery.From.UserName

			if !usernameExists(srv, username) {
				msg := tgbotapi.NewMessage(update.CallbackQuery.Message.Chat.ID, "⛔ Siz ro'yxatdan o'tmagansiz.")
				bot.Send(msg)
				continue
			}

			category := update.CallbackQuery.Data

			result, err := getCountByCategoryAndAngar(srv, category)
			if err != nil {
				msg := tgbotapi.NewMessage(update.CallbackQuery.Message.Chat.ID, fmt.Sprintf("Xatolik: %v", err))
				bot.Send(msg)
				continue
			}

			msg := tgbotapi.NewMessage(update.CallbackQuery.Message.Chat.ID, result)
			msg.ParseMode = "Markdown"
			bot.Send(msg)

			// Optional: answer the callback to remove the "loading" spinner
			bot.Request(tgbotapi.NewCallback(update.CallbackQuery.ID, "✅"))
		}
	}

}

func categoryKeyboard(categories []string) tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, cat := range categories {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(cat, cat),
		))
	}
	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

func getCategories(srv *sheets.Service) ([]string, error) {
	readRange := SheetName + "!A1:Z1000"
	resp, err := srv.Spreadsheets.Values.Get(SpreadsheetID, readRange).Do()
	if err != nil {
		return nil, err
	}

	if len(resp.Values) < 1 {
		return nil, fmt.Errorf("jadval bo‘sh")
	}

	header := resp.Values[0]
	var colIndex int = -1
	for i, val := range header {
		if strings.TrimSpace(fmt.Sprintf("%v", val)) == "Mahsulot Kategoriyasi" {
			colIndex = i
			break
		}
	}
	if colIndex == -1 {
		return nil, fmt.Errorf("mahsulot Kategoriyasi ustuni topilmadi")
	}

	categoryMap := make(map[string]bool)
	for _, row := range resp.Values[1:] {
		if len(row) > colIndex {
			cat := strings.ToUpper(strings.TrimSpace(fmt.Sprintf("%v", row[colIndex])))
			if cat != "" {
				categoryMap[cat] = true
			}
		}
	}

	var categories []string
	for k := range categoryMap {
		categories = append(categories, k)
	}

	return categories, nil
}

func getCountByCategoryAndAngar(srv *sheets.Service, category string) (string, error) {
	readRange := SheetName + "!A1:Z1000"
	resp, err := srv.Spreadsheets.Values.Get(SpreadsheetID, readRange).Do()
	if err != nil {
		return "", err
	}

	var catIdx, angarIdx, modelIdx, soniIdx int = -1, -1, -1, -1
	if len(resp.Values) == 0 {
		return "", fmt.Errorf("jadval bosh")
	}

	header := resp.Values[0]
	for i, val := range header {
		switch strings.TrimSpace(fmt.Sprintf("%v", val)) {
		case "Mahsulot Kategoriyasi":
			catIdx = i
		case "Sklad":
			angarIdx = i
		case "Model":
			modelIdx = i
		case "Soni":
			soniIdx = i
		}
	}

	if catIdx == -1 || angarIdx == -1 || modelIdx == -1 || soniIdx == -1 {
		return "", fmt.Errorf("zarur ustunlar topilmadi: 'Mahsulot Kategoriyasi', 'Sklad', 'Model', 'Soni'")
	}

	// Nested map: Sklad -> Model -> Count (based on Soni)
	data := make(map[string]map[string]int)
	for _, row := range resp.Values[1:] {
		if len(row) > catIdx && len(row) > angarIdx && len(row) > modelIdx && len(row) > soniIdx {
			cat := strings.ToUpper(strings.TrimSpace(fmt.Sprintf("%v", row[catIdx])))
			angar := strings.TrimSpace(fmt.Sprintf("%v", row[angarIdx]))
			model := strings.TrimSpace(fmt.Sprintf("%v", row[modelIdx]))
			soniStr := strings.TrimSpace(fmt.Sprintf("%v", row[soniIdx]))

			if cat != category || angar == "" || model == "" || soniStr == "" {
				continue
			}

			var soni int
			fmt.Sscanf(soniStr, "%d", &soni)
			if soni == 0 {
				continue
			}

			if data[angar] == nil {
				data[angar] = make(map[string]int)
			}
			data[angar][model] += soni
		}
	}

	if len(data) == 0 {
		return fmt.Sprintf("*%s* kategoriyasi uchun ma'lumot topilmadi", category), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("*%s* bo‘yicha ANGAR va Model kesimidagi soni:\n\n", category))
	for angar, models := range data {
		var tempSB strings.Builder
		for model, count := range models {
			if count < 3 {
				// Skip this model
				continue
			}

			var result string
			switch {
			case count < 50:
				result = fmt.Sprintf("🔵 %d ta", count)
			case count < 100:
				result = "🟡 50+"
			default:
				result = "🟢 100+"
			}

			tempSB.WriteString(fmt.Sprintf("  📦 %s: %s\n", model, result))
		}

		// Only write angar if there's at least one model printed
		if tempSB.Len() > 0 {
			sb.WriteString(fmt.Sprintf("🏠 *%s*:\n", angar))
			sb.WriteString(tempSB.String())
			sb.WriteString("\n")
		}
	}

	return sb.String(), nil
}
