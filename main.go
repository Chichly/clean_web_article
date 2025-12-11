package main

import (
	"net/http"

	"github.com/PuerkitoBio/goquery"
	"github.com/gin-gonic/gin"
)

const API_KEY = "demo_12345" // clé API

func extractHandler(c *gin.Context) {
	// Vérification de la clé API
	key := c.Query("key")
	if key != API_KEY {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or missing API key"})
		return
	}


	// Lire l'URL
	url := c.Query("url")
	if url == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing url parameter"})
		return
	}

	resp, err := http.Get(url)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch url"})
		return
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to parse page"})
		return
	}

	title := doc.Find("title").First().Text()
	text := doc.Find("p").Text()

	c.JSON(http.StatusOK, gin.H{
		"title": title,
		"text":  text,
	})
}

func main() {
	router := gin.Default()
	router.GET("/extract", extractHandler)
	router.Run(":8080") // Render utilise ça
}
