package main

import (
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	everquest "github.com/Mortimus/goEverquest"
	"github.com/gin-gonic/gin"
)

var guild everquest.Guild
var start time.Time
var rosterTime time.Time
var rosterPath string
var requests uint
var appErrors uint

func main() {
	start = time.Now()
	rosterPath = "Vets of Norrath_aradune-20211016-165944.txt"
	info, err := os.Stat(rosterPath)
	if err != nil {
		panic(err)
	}
	rosterTime = info.ModTime()
	err = guild.LoadFromPath(rosterPath, nil)
	if err != nil {
		panic(err)
	}
	// todo: middleware for logging request count
	r := gin.Default()
	r.GET("/char/:character", charHandler)
	r.GET("/main/:character", mainHandler)
	r.GET("/class/:class", classHandler)
	r.GET("/health", healthHandler)
	r.Run() // listen and serve on 0.0.0.0:8080 (for windows "localhost:8080")
}

func healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"uptime":          time.Since(start).String(),
		"requestsHandled": requests,
		"lastRoster":      rosterTime.String(),
		"rosterPath":      rosterPath,
		"errors":          appErrors,
	})
}

func charHandler(c *gin.Context) {
	player, err := guild.GetMemberByName(c.Param("character"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Player not found",
		})
		appErrors++
		return
	}
	c.JSON(http.StatusOK, player)
}

func mainHandler(c *gin.Context) {
	player, err := guild.GetMemberByName(c.Param("character"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Player not found",
		})
		appErrors++
		return
	}
	main, err := findMain(player)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": err.Error(),
		})
		appErrors++
		return
	}
	c.JSON(http.StatusOK, main)
}

func classHandler(c *gin.Context) {
	classes := []string{c.Param("class")}
	players := getClassMembers(classes)
	if len(players) <= 0 {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "No players for provided class",
		})
		appErrors++
		return
	}
	c.JSON(http.StatusOK, players)
}

func findMain(player everquest.GuildMember) (everquest.GuildMember, error) {
	if !player.Alt { // Not an alt, so has to be a main
		return player, nil
	}
	lNote := strings.ToLower(player.PublicNote)
	if lNote == "" {
		return player, errors.New(player.Name + " has no PublicNote set to determine main")
	}
	if !strings.Contains(lNote, "nd main") && !strings.Contains(lNote, " alt") {
		return player, errors.New(player.Name + "'s PublicNote does not mention nd main or Alt: " + player.PublicNote)
	}
	parseName := strings.Split(player.PublicNote, " ")
	if len(parseName) <= 1 {
		return player, errors.New(player.Name + " cannot split on PublicNote space: " + player.PersonalNote)
	}
	cleanName := strings.Replace(parseName[0], "'", "", -1)
	return guild.GetMemberByName(strings.Title(cleanName))
}

func getClassMembers(classes []string) []everquest.GuildMember {
	var players []everquest.GuildMember
	for _, player := range guild.Members {
		if player.IsClass(classes) {
			players = append(players, player)
		}
	}
	return players
}
