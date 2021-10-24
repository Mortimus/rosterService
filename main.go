package main

import (
	"errors"
	"fmt"
	"io/fs"
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
var GuildName string
var ServerName string

// const GuildName = "Vets of Norrath"
const Port = ":8080"
const LogPath = "rosterService.log"
const DumpPath = "./"

func main() {
	f, err := os.OpenFile(LogPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	defer f.Close()

	log.SetOutput(f)
	log.SetFlags(log.Lshortfile | log.Ldate | log.Ltime | log.LUTC | log.Lmsgprefix)

	start = time.Now()
	rosterPath, err = findDump(DumpPath)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	GuildName, ServerName = decodeDump(rosterPath)
	err = guild.LoadFromPath(rosterPath, log.Default())
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
	r.GET("/logs", logHandler)
	r.GET("/guildname", guildNameHandler)
	r.GET("/servername", serverNameHandler)
	r.Run(Port)
}

func findDump(path string) (string, error) {
	var files []string

	err := filepath.WalkDir(path, func(path string, d fs.DirEntry, err error) error {
		if strings.HasSuffix(d.Name(), ".txt") {
			files = append(files, d.Name())
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(files) <= 0 {
		return "", errors.New("cannot find a recent guild dump")
	}
	return files[len(files)-1], nil // return last file - should be latest
}

func decodeDump(dumpname string) (guildname string, servername string) {
	dump := strings.Split(dumpname, "_")
	guildname = dump[0]
	dumpy := strings.Split(dump[1], "-")
	servername = dumpy[0]
	return guildname, servername
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
		"guild":           GuildName,
		"server":          ServerName,
	})
}

func serverNameHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"server": ServerName,
	})
}

func guildNameHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"guild": GuildName,
	})
}

// Return a single character
func charHandler(c *gin.Context) {
	player, err := guild.GetMemberByName(c.Param("character"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Player not found",
		})
		log.Printf("%s not found, requested by %s - %s", c.Param("character"), c.ClientIP(), c.Request.UserAgent())
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
		log.Printf("%s not found, requested by %s - %s", c.Param("character"), c.ClientIP(), c.Request.UserAgent())
		appErrors++
		return
	}
	main, err := findMain(player)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": err.Error(),
		})
		log.Printf("%s, requested by %s - %s", err.Error(), c.ClientIP(), c.Request.UserAgent())
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
		log.Printf("%s has no players, requested by %s - %s", c.Param("class"), c.ClientIP(), c.Request.UserAgent())
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
		log.Printf("%s file not found, requested by %s - %s", c.Param("file"), c.ClientIP(), c.Request.UserAgent())
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
		log.Printf("%s invalid guild format, requested by %s - %s", filename, c.ClientIP(), c.Request.UserAgent())
		appErrors++
		return
	}
	dir, err := ioutil.TempDir("", "guildUploads")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		log.Printf("%s :: creating tmp dir, requested by %s - %s", err.Error(), c.ClientIP(), c.Request.UserAgent())
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
		log.Printf("%s :: loading guild, requested by %s - %s", err.Error(), c.ClientIP(), c.Request.UserAgent())
		appErrors++
		return
	}
	guild = everquest.MergeGuilds(guild, tempGuild)
	err = guild.WriteToPath(filename)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		log.Printf("%s :: writing guild, requested by %s - %s", err.Error(), c.ClientIP(), c.Request.UserAgent())
		appErrors++
		return
	}
	err = os.Remove(rosterPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		log.Printf("%s :: removing guild, requested by %s - %s", err.Error(), c.ClientIP(), c.Request.UserAgent())
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

func logHandler(c *gin.Context) {
	f, err := os.ReadFile(LogPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		log.Printf("%s :: reading log, requested by %s - %s", err.Error(), c.ClientIP(), c.Request.UserAgent())
		appErrors++
		return
	}
	c.String(http.StatusOK, string(f))
}
