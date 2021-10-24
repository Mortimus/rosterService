package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	everquest "github.com/Mortimus/goEverquest"
	"github.com/gin-gonic/gin"
)

var guild everquest.Guild
var start time.Time
var rosterPath string
var requests uint
var appErrors uint

const GuildName = "Vets of Norrath"
const Port = ":8080"

func main() {
	start = time.Now()
	rosterPath = "Vets of Norrath_aradune-20211016-165944.txt"
	err := guild.LoadFromPath(rosterPath, nil)
	if err != nil {
		panic(err)
	}
	// todo: middleware for logging request count
	r := gin.Default()
	r.POST("/upload", dumpHandler)
	r.GET("/char/:character", charHandler)
	r.GET("/main/:character", mainHandler)
	r.GET("/class/:class", classHandler)
	r.GET("/guild", guildHandler)
	r.GET("/health", healthHandler)
	r.Run(Port)
}

func healthHandler(c *gin.Context) {
	fi, err := os.Stat(rosterPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		appErrors++
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"uptime":          time.Since(start).String(),
		"requestsHandled": requests,
		"lastRoster":      fi.ModTime(),
		"rosterPath":      rosterPath,
		"rosterCount":     fmt.Sprintf("%d", len(guild.Members)),
		"rosterFileSize":  fmt.Sprintf("%d", fi.Size()),
		"errors":          appErrors,
	})
}

// Return a single character
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

// Return a character's main
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

// Return all characters of supplied class
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

// find main based on character's details
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

// get all characters of class
func getClassMembers(classes []string) []everquest.GuildMember {
	var players []everquest.GuildMember
	for _, player := range guild.Members {
		if player.IsClass(classes) {
			players = append(players, player)
		}
	}
	return players
}

// take a guild dump and merge with existing guild
func dumpHandler(c *gin.Context) {
	if c.Params.ByName("file") == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "missing file",
		})
		appErrors++
		return
	}
	file, _ := c.FormFile("file")
	log.Println(file.Filename)
	filename := file.Filename

	// Check if this is filename is formatted correctly
	if filepath.Ext(filename) != ".txt" || !strings.HasPrefix(filename, GuildName) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid guild format",
		})
		appErrors++
		return
	}
	dir, err := ioutil.TempDir("", "guildUploads")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		appErrors++
		return
	}
	defer os.RemoveAll(dir)

	// Upload the file to specific dst.
	c.SaveUploadedFile(file, dir+"/"+filename)
	var tempGuild everquest.Guild
	err = tempGuild.LoadFromPath(dir+"/"+filename, log.Default())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		appErrors++
		return
	}
	guild = everquest.MergeGuilds(guild, tempGuild)
	err = guild.WriteToPath(filename)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		appErrors++
		return
	}
	err = os.Remove(rosterPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		appErrors++
		return
	}
	rosterPath = filename
	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("'%s' uploaded and merged successfully", file.Filename),
	})
}

// return all guild members
func guildHandler(c *gin.Context) {
	c.JSON(http.StatusOK, guild.Members)
}
