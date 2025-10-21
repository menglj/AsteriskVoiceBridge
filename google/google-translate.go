/*
 * Google Translate API integration for AsteriskVoiceBridge
 *
 * Copyright (C) 2025, Sangoma Technologies Corporation
 *
 * This program is free software, distributed under the terms of
 * the GNU AFFERO General Public License Version 3. See the LICENSE file
 * at the top of the source tree.
 */

package google

import (
	"context"
	"os"
	"strings"

	"cloud.google.com/go/translate"
	"golang.org/x/text/language"
)

// TranslationCallback is called when translation is complete
type TranslationCallback func(callid string, translatedText string, sourceLanguage string, targetLanguage string) bool

// GoogleTranslateProvider handles Google Cloud Translation API
type GoogleTranslateProvider struct {
	client           *translate.Client
	translationCB    TranslationCallback
	sourceLanguage   string
	targetLanguage   string
}

// NewGoogleTranslateProvider creates a new Google Translate provider
func NewGoogleTranslateProvider() (*GoogleTranslateProvider, bool) {
	ctx := context.Background()
	
	client, err := translate.NewClient(ctx)
	if err != nil {
		log.Error("Failed to create Google Translate client", "error", err)
		return nil, false
	}

	// Get source and target languages from environment variables
	sourceLang := os.Getenv("TRANSLATE_SOURCE_LANGUAGE")
	if sourceLang == "" {
		sourceLang = "en-US" // Default to English
	}
	
	targetLang := os.Getenv("TRANSLATE_TARGET_LANGUAGE")
	if targetLang == "" {
		targetLang = "zh-CN" // Default to Chinese
	}

	provider := &GoogleTranslateProvider{
		client:         client,
		sourceLanguage: sourceLang,
		targetLanguage: targetLang,
	}

	log.Info("Google Translate provider created", 
		"source_language", sourceLang, 
		"target_language", targetLang)

	return provider, true
}

// SetTranslationCallback sets the callback for translation results
func (g *GoogleTranslateProvider) SetTranslationCallback(callback TranslationCallback) {
	g.translationCB = callback
}

// TranslateText translates text from source language to target language
func (g *GoogleTranslateProvider) TranslateText(callid string, text string) bool {
	if text == "" {
		return true
	}

	// Clean up the text
	text = strings.TrimSpace(text)
	if text == "" {
		return true
	}

	log.Info("Translating text", 
		"callid", callid, 
		"text", text, 
		"source", g.sourceLanguage, 
		"target", g.targetLanguage)

	ctx := context.Background()

	// Convert language codes to language.Tag
	sourceTag, err := language.Parse(g.sourceLanguage)
	if err != nil {
		log.Error("Invalid source language", "language", g.sourceLanguage, "error", err)
		return false
	}

	targetTag, err := language.Parse(g.targetLanguage)
	if err != nil {
		log.Error("Invalid target language", "language", g.targetLanguage, "error", err)
		return false
	}

	// Perform translation
	translations, err := g.client.Translate(ctx, []string{text}, targetTag, &translate.Options{
		Source: sourceTag,
		Format: translate.Text,
	})
	if err != nil {
		log.Error("Translation failed", "error", err)
		return false
	}

	if len(translations) == 0 {
		log.Error("No translation result")
		return false
	}

	translatedText := translations[0].Text
	log.Info("Translation completed", 
		"callid", callid, 
		"original", text, 
		"translated", translatedText)

	// Call the callback with the translated text
	if g.translationCB != nil {
		return g.translationCB(callid, translatedText, g.sourceLanguage, g.targetLanguage)
	}

	return true
}

// SetLanguages updates the source and target languages
func (g *GoogleTranslateProvider) SetLanguages(sourceLang, targetLang string) {
	g.sourceLanguage = sourceLang
	g.targetLanguage = targetLang
	log.Info("Translation languages updated", 
		"source", sourceLang, 
		"target", targetLang)
}

// GetSourceLanguage returns the current source language
func (g *GoogleTranslateProvider) GetSourceLanguage() string {
	return g.sourceLanguage
}

// GetTargetLanguage returns the current target language
func (g *GoogleTranslateProvider) GetTargetLanguage() string {
	return g.targetLanguage
}

// TranslateTextSync 同步翻译(用于坐席文字回应)
func (g *GoogleTranslateProvider) TranslateTextSync(text, sourceLang, targetLang string) string {
	if text == "" {
		return ""
	}

	// Clean up the text
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	log.Info("Sync translating text", 
		"text", text, 
		"source", sourceLang, 
		"target", targetLang)

	ctx := context.Background()

	// Convert language codes to language.Tag
	sourceTag, err := language.Parse(sourceLang)
	if err != nil {
		log.Error("Invalid source language", "language", sourceLang, "error", err)
		return text
	}

	targetTag, err := language.Parse(targetLang)
	if err != nil {
		log.Error("Invalid target language", "language", targetLang, "error", err)
		return text
	}

	// Perform translation
	translations, err := g.client.Translate(ctx, []string{text}, targetTag, &translate.Options{
		Source: sourceTag,
		Format: translate.Text,
	})
	if err != nil {
		log.Error("Sync translation failed", "error", err)
		return text
	}

	if len(translations) == 0 {
		log.Error("No sync translation result")
		return text
	}

	translatedText := translations[0].Text
	log.Info("Sync translation completed", 
		"original", text, 
		"translated", translatedText)

	return translatedText
}

// GetSupportedLanguages returns a list of supported language codes
func GetSupportedLanguages() []string {
	return []string{
		"en-US", "en-GB", "en-AU", // English variants
		"zh-CN", "zh-TW", "zh-HK", // Chinese variants
		"ja-JP", // Japanese
		"ko-KR", // Korean
		"es-ES", "es-MX", "es-AR", // Spanish variants
		"fr-FR", "fr-CA", // French variants
		"de-DE", "de-AT", "de-CH", // German variants
		"it-IT", // Italian
		"pt-BR", "pt-PT", // Portuguese variants
		"ru-RU", // Russian
		"ar-SA", "ar-EG", // Arabic variants
		"hi-IN", // Hindi
		"th-TH", // Thai
		"vi-VN", // Vietnamese
		"nl-NL", // Dutch
		"sv-SE", // Swedish
		"no-NO", // Norwegian
		"da-DK", // Danish
		"fi-FI", // Finnish
		"pl-PL", // Polish
		"tr-TR", // Turkish
		"cs-CZ", // Czech
		"hu-HU", // Hungarian
		"ro-RO", // Romanian
		"bg-BG", // Bulgarian
		"hr-HR", // Croatian
		"sk-SK", // Slovak
		"sl-SI", // Slovenian
		"et-EE", // Estonian
		"lv-LV", // Latvian
		"lt-LT", // Lithuanian
		"uk-UA", // Ukrainian
		"el-GR", // Greek
		"he-IL", // Hebrew
		"fa-IR", // Persian
		"ur-PK", // Urdu
		"bn-BD", // Bengali
		"ta-IN", // Tamil
		"te-IN", // Telugu
		"ml-IN", // Malayalam
		"kn-IN", // Kannada
		"gu-IN", // Gujarati
		"pa-IN", // Punjabi
		"or-IN", // Odia
		"as-IN", // Assamese
		"ne-NP", // Nepali
		"si-LK", // Sinhala
		"my-MM", // Burmese
		"km-KH", // Khmer
		"lo-LA", // Lao
		"ka-GE", // Georgian
		"am-ET", // Amharic
		"sw-KE", // Swahili
		"zu-ZA", // Zulu
		"af-ZA", // Afrikaans
		"sq-AL", // Albanian
		"mk-MK", // Macedonian
		"sr-RS", // Serbian
		"bs-BA", // Bosnian
		"mt-MT", // Maltese
		"is-IS", // Icelandic
		"ga-IE", // Irish
		"cy-GB", // Welsh
		"eu-ES", // Basque
		"ca-ES", // Catalan
		"gl-ES", // Galician
	}
}
