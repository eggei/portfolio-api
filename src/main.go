package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"

	dialogflow "cloud.google.com/go/dialogflow/apiv2"
	structpb "github.com/golang/protobuf/ptypes/struct"
	"google.golang.org/api/option"
	dialogflowpb "google.golang.org/genproto/googleapis/cloud/dialogflow/v2"
)

// code taken from https://chatbotslife.com/golang-dialogflow-d32c4be5e124

// DialogflowProessor has all the information for connecting with Dialogflow
type DialogflowProcessor struct {
    projectID string
    authJSONFilePath string
    lang string
    timeZone string
    sessionClient *dialogflow.SessionsClient
    ctx context.Context
}

// NLPResponse is the struct for the response
type NLPResponse struct {
    Intent string `json:"intent"`
    Confidence float32 `json:"confidence"`
    Entities map[string]string `json:"entities"`
    FulfillmentMessages []*dialogflowpb.Intent_Message `json:"fulfillmentMessages"`
}

var dp DialogflowProcessor

func (dp *DialogflowProcessor) init (a ...string) (err error) {
    dp.projectID = a[0]
    dp.authJSONFilePath = a[1]
    dp.lang = a[2]
    dp.timeZone = a[3]

    // Auth process: https://dialogflow.com/docs/reference/v2-auth-setup

    dp.ctx = context.Background()
    sessionClient, err := dialogflow.NewSessionsClient(
        dp.ctx,
        option.WithCredentialsFile(dp.authJSONFilePath),
    )
    if err != nil {
        log.Println("Error in auth with Dialogflow")
    }
    dp.sessionClient = sessionClient
    return
}

func (dp *DialogflowProcessor) processNLP(rawMessage string, username string) (r NLPResponse) {
    sessionID := username
    request := dialogflowpb.DetectIntentRequest{
        Session: fmt.Sprintf("projects/%s/agent/sessions/%s", dp.projectID, sessionID),
        QueryInput: &dialogflowpb.QueryInput{
            Input: &dialogflowpb.QueryInput_Text{
                Text: &dialogflowpb.TextInput {
                    Text: rawMessage,
                    LanguageCode: dp.lang,
                },
            },
        },
        QueryParams: &dialogflowpb.QueryParameters{
            TimeZone: dp.timeZone,
        },
    }
    response, err := dp.sessionClient.DetectIntent(dp.ctx, &request)
    if err != nil {
        log.Printf("Error in communication with Dialogflow %s", err.Error())
        return
    }
    queryResult := response.GetQueryResult()
    if queryResult.Intent != nil {
        r.Intent = queryResult.Intent.DisplayName
        r.Confidence = float32(queryResult.IntentDetectionConfidence)
        r.FulfillmentMessages = queryResult.FulfillmentMessages
    }
    r.Entities = make(map[string]string)
    params := queryResult.Parameters.GetFields()
    if len(params) > 0 {
        for paramName, p := range params {
            fmt.Printf("Param %s: %s (%s)", paramName, p.GetStringValue(), p.String())
            extractedValue := extractDialogflowEntities(p)
            r.Entities[paramName] = extractedValue
        }
    }
    return
}

func extractDialogflowEntities(p *structpb.Value) (extractedEntity string) {
    kind := p.GetKind()
    switch kind.(type) {
    case *structpb.Value_StringValue:
        return p.GetStringValue()
    case *structpb.Value_NumberValue:
        return strconv.FormatFloat(p.GetNumberValue(), 'f', 6, 64)
    case *structpb.Value_BoolValue:
        return strconv.FormatBool(p.GetBoolValue())
    case *structpb.Value_StructValue:
        s := p.GetStructValue()
        fields := s.GetFields()
        extractedEntity = ""
        for key, value := range fields {
            if key == "amount" {
                extractedEntity = fmt.Sprintf("%s%s", extractedEntity, strconv.FormatFloat(value.GetNumberValue(), 'f', 6, 64))
            }
            if key == "unit" {
                extractedEntity = fmt.Sprintf("%s%s", extractedEntity, value.GetStringValue())
            }
            if key == "date_time" {
                extractedEntity = fmt.Sprintf("%s%s", extractedEntity, value.GetStringValue())
            }
            // @TODO: other entity types can be added here
        }
        return extractedEntity
    case *structpb.Value_ListValue:
        list := p.GetListValue()
        if len(list.GetValues()) > 1 {
            // @TODO: Extract more values
        }
        extractedEntity = extractDialogflowEntities(list.GetValues()[0])
        return extractedEntity
    default:
        return ""
    }
}

func intentRequestHandler(w http.ResponseWriter, r *http.Request) {
    // TODO: * is not a good implementation of enabling CORS
    w.Header().Set("Access-Control-Allow-Origin", "*")
    w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
    w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")

    // log request
    log.Printf("--- %s request from %s", r.Method, r.Host) 

    if r.Method != "POST" {
        http.Error(w, "Method is not supported.", http.StatusNotFound)
        return
    }
    if r.Method == "POST" {
        body, err := ioutil.ReadAll((r.Body))
        if err != nil {
            http.Error(
                w, 
                "Error reading reequest body",
                http.StatusInternalServerError,
            )
        }
        type userQuestion struct {
            Question string `json:"question"`
        }
        var m userQuestion
        err = json.Unmarshal(body, &m)
        if err != nil {
            panic(err)
        }
        
        // Use NLP
        response := dp.processNLP(m.Question, r.RemoteAddr) // use IP address as session id
        fmt.Printf("%#v", response)
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(response)
    }
}

func main() {
    projectID := "chatfolio-q9qs"
    authFile := "chatfolio-q9qs-b17009ca2aa1.json" 
    dp.init(
        projectID, 
        authFile, 
        "en", 
        "America/Boston",
    )

    http.HandleFunc("/", func (w http.ResponseWriter, r *http.Request) { 
            json.NewEncoder(w).Encode("Healthy")  
        }) 
    http.HandleFunc("/api/get-intent", intentRequestHandler)

    fmt.Printf("\n >>>>>>> Starting server at port 8888\n\n")
    if err := http.ListenAndServe(":8888", nil); err != nil {
        log.Fatal(err)
    }
}