package main

import (
    "github.com/codegangsta/martini"
    "encoding/base64"
    "os"
    "io/ioutil"
    "net/http"
    "time"
    "crypto/sha256"
    "log"
    "bufio"
    "code.google.com/p/gcfg"
    "strconv"
    "strings"
)

//Maps keys to teams
var teams = make(map[string]string)
//Configuration
var config = struct {
    Teams struct {
        Name []string
        Key []string
    }
    Game struct {
        Start_Date string
        End_Date string
        Start_Time string
        End_Time string
        Num_Pictures string
    }
}{}
//Start and end times
var starttime time.Time
var endtime time.Time
//Number of pictures each team can take
var numpictures int

//Format string for time printing
const timeLayout = "01/02/2006 at 15:04"

//Checks whether or not we are in the hours for photohunt
//-1 means we are before photohunt,
// 0 means we are in photohunt
// 1 means photohunt has ended
func timeCheck() int {
    if time.Now().Before(starttime) {
        return -1
    }
    if time.Now().After(endtime) {
        return 1
    }
    return 0
}

func getNumPicsForTeam(team string) int {
    files, err := ioutil.ReadDir(team)
    if err != nil {
        return 0
    }
    return len(files)
}

func appendToFile(filename string, line string) error {
    _, err := os.Stat(filename)
    var file *os.File
    if os.IsNotExist(err) {
        file, err = os.Create(filename)
    } else {
        file, err = os.OpenFile(filename, os.O_RDWR|os.O_APPEND, 0755)
    }
    if err != nil {
        return err
    }
    defer file.Close()
    _, err = file.WriteString(line)
    if err != nil {
        return err
    }
    return nil
}

// readLines reads a whole file into memory
// and returns a slice of its lines.
func readLines(path string) ([]string, error) {
  file, err := os.Open(path)
  if err != nil {
    return nil, err
  }
  defer file.Close()

  var lines []string
  scanner := bufio.NewScanner(file)
  for scanner.Scan() {
    lines = append(lines, scanner.Text())
  }
  return lines, scanner.Err()
}

//Accepts an uploaded file. Requires two url parameters, key and hash.
//key is a team's given key to identify the team
//hash is a sha256 hash of the binary data of the image, 
//encoded in base64url
//The body should contain a base64 png file
func uploadPicture(writer http.ResponseWriter,
        request *http.Request, params martini.Params) (int, string) {

    timecomparison := timeCheck()
    if timecomparison == -1 {
        log.Println("Photohunt hasn't started yet")
        return 500, "Photohunt hasn't started yet"
    }
    if timecomparison == 1 {
        log.Println("Photohunt is over")
        return 500, "Photohunt is over"
    }

    //Read the body
    o, _ := ioutil.ReadAll(request.Body)
    defer request.Body.Close()

    //Get the url parameters
    v := request.URL.Query()

    //Check if the required parameters are present
    key, ok := v["key"]
    if !ok {
        log.Println("Missing key")
        return 500, "Missing key"
    }
    givenhash, ok := v["hash"]
    if !ok {
        log.Println("Missing hash")
        return 500, "Missing hash"
    }
    extension, ok := v["fileextension"]
    if !ok {
        log.Println("Missing fileextension")
        return 500, "Missing fileextension"
    }

    //Check if the key supplied belongs to any teams.
    //Record which team it belongs to
    team, ok := teams[key[0]]
    if !ok {
        log.Println("Invalid Key")
        return 500, "Invalid key"
    }

    log.Printf("File upload made by team %s\n", team)

    numPics := getNumPicsForTeam(team)
    if numPics >= numpictures {
        log.Println("Team has uploded max number of pictures!")
        return 500, "Upload limit reached"
    }

    //Decode the image into a byte[]
    data, err := base64.StdEncoding.DecodeString(string(o))
    if err != nil {
        log.Printf("Error decoding image. Upload aborted\n")
        return 500, "Couldn't decode image"
    }

    //hash the image, encode it with base64url
    hasher := sha256.New()
    hasher.Write(data)
    generatedhash := base64.URLEncoding.EncodeToString(hasher.Sum(nil))
    log.Printf("Comparing \"%s\" to \"%s\"\n",
            strings.TrimSpace(generatedhash),
            strings.TrimSpace(givenhash[0]))

    //Check that the generate hash matches the given one
    if strings.TrimSpace(generatedhash) !=
            strings.TrimSpace(givenhash[0]) {
        log.Printf("Image corrupted\n")
        return 500, "Error: data corrupted"
    }

    //Check that we haven't received this hash before
    lines, err := readLines("hashes/" + team + ".hash")
    if err != nil && !os.IsNotExist(err) {
        log.Println("Error reading hash file")
        return 500, "Internal server error"
    }

    for _, line := range(lines) {
        if generatedhash == strings.TrimSpace(line) {
            log.Println("Duplicate file. Upload aborted")
            return 500, "Duplicate file"
        }
    }

    //Write down the hash
    _, err = os.Stat("hashes")
    if os.IsNotExist(err) {
        err = os.Mkdir("hashes", 0755)
    }
    if err != nil {
        log.Printf("Error creating hash folder. Upload aborted\n")
        return 500, "Internal server error"
    }
    log.Printf("Making " + "hashes/" + team + ".hash\n")
    err =
        appendToFile("hashes/" + team + ".hash", generatedhash + "\n")
    if err != nil {
        log.Printf("Error creating or writing to hash file\n")
        return 500, "Internal server error"
    }

    //Make the directory, make the file
    _, err = os.Stat(team)
    if os.IsNotExist(err) {
        err = os.Mkdir(team, 0755)
    }
    file, err := os.Create(team + "/" + time.Now().Format(time.RFC850) + "." + extension[0])
    if err != nil {
        log.Printf("Error creating file. Upload aborted\n")
        return 500, "Internal server error"
    }

    //Close the file at the end of this
    defer file.Close()

    //Write the image in to the file
    _, err = file.Write(data)
    if err != nil {
        log.Printf("Error writing to file. Upload aborted\n")
        return 500, "Internal server error"
    }

    //Mission accomplished
    log.Printf("Upload successful\n")
    return 200, "File received"
}

func getTimes(writer http.ResponseWriter,
        request *http.Request, params martini.Params) (int, string) {
    //Get the url parameters
    v := request.URL.Query()

    //Check if the required parameters are present
    key, ok := v["key"]
    if !ok {
        return 500, "Missing key"
    }

    //Check if the key supplied belongs to any teams.
    _, ok = teams[key[0]]
    if !ok {
        return 500, "Invalid key"
    }

    //Return the times
    return 200,
        starttime.Format(timeLayout) + " until " + endtime.Format(timeLayout)
}

func getNumPictures(writer http.ResponseWriter,
        request *http.Request, params martini.Params) (int, string) {
    //Get the url parameters
    v := request.URL.Query()

    //Check if the required parameters are present
    key, ok := v["key"]
    if !ok {
        return 500, "Missing key"
    }

    //Check if the key supplied belongs to any teams
    team, ok := teams[key[0]]
    if !ok {
        log.Println("Invalid Key")
        return 500, "Invalid key"
    }

    //and get the number of pictures uploaded
    numPicsUploaded := getNumPicsForTeam(team)

    log.Println("Returning: " + strconv.Itoa(numPicsUploaded) + " / " + strconv.Itoa(numpictures))

    return 200,
        strconv.Itoa(numPicsUploaded) + " / " + strconv.Itoa(numpictures)
}

func getTeam(writer http.ResponseWriter,
        request *http.Request, params martini.Params) (int, string) {
    //Get the url parameters
    v := request.URL.Query()

    //Check if the required parameters are present
    key, ok := v["key"]
    if !ok {
        return 500, "Missing key"
    }

    //Check if the key supplied belongs to any teams.
    team, ok := teams[key[0]]
    if !ok {
        return 500, "Invalid key"
    }

    return 200, team;
}

func main() {
    //Check command line arguments
    args := os.Args
    if len(args) == 1 || args[1] == "-h" {
        log.Println("Usage: photohunt <config-file.gcfg>")
        os.Exit(0)
    }
    if len(args) != 2 {
        log.Println("Usage: photohunt <config-file.gcfg>")
        os.Exit(1)
    }

    //Open the config file
    file, fiError := os.Open(args[1])
    if fiError != nil {
        log.Println("Error opening config file")
        os.Exit(1)
    }
    //Close it when we're done
    defer file.Close()

    //Read in the config file
    var lines string
    scanner := bufio.NewScanner(file)
    for scanner.Scan() {
        lines += scanner.Text() + "\n"
    }
    if scanner.Err() != nil {
        log.Println("Scanner error")
        os.Exit(1)
    }

    //Parse the config file
    err := gcfg.ReadStringInto(&config, lines)
    if err != nil {
        log.Println("Error parsing config")
        os.Exit(1)
    }

    //Error check the config file
    if len(config.Teams.Name) > len(config.Teams.Key) {
        log.Println("Error: more team names than keys in config file")
        os.Exit(1)
    }

    if len(config.Teams.Name) < len(config.Teams.Key) {
        log.Println("Error: more keys than team names in config file")
        os.Exit(1)
    }

    //Add and print key/team mappings
    for i := 0; i < len(config.Teams.Name); i++ {
        log.Printf("Adding team: %s\n", config.Teams.Name[i])
        log.Printf("With key:    %s\n", config.Teams.Key[i])
        teams[config.Teams.Key[i]] = config.Teams.Name[i]
    }

    //Load in start/end times
    starttime, err = time.Parse(timeLayout + " (MST)",
            config.Game.Start_Date + " at " + config.Game.Start_Time + " (EDT)")
    if err != nil {
        log.Println(err)
        os.Exit(1)
    }
    endtime, err = time.Parse(timeLayout + " (MST)",
            config.Game.End_Date + " at " + config.Game.End_Time + " (EDT)")
    if err != nil {
        log.Println(err)
        os.Exit(1)
    }

    //Print the times
    log.Printf("Photohunt will run from %s until %s\n",
            starttime.Format(timeLayout), endtime.Format(timeLayout))

    numpictures, err = strconv.Atoi(config.Game.Num_Pictures)
    if err != nil {
        log.Println("Error parsing num-pictures from config")
        os.Exit(1)
    }

    //Load/run martini
    m := martini.Classic()
    m.Post("/upload", uploadPicture)
    m.Get("/times", getTimes)
    m.Get("/numpics", getNumPictures)
    m.Get("/team", getTeam)
    //m.Run()
    log.Println("Listening for https on port 3912")
    if err := http.ListenAndServeTLS(":3912", "server.crt", "server.key", m); err != nil {
        log.Fatal(err)
    }
}
