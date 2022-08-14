package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"

	"net/http"
	"os"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gopkg.in/natefinch/lumberjack.v2"
)

// var awsId string
// var awsSecret string
// var hostedZoneiD string
// var domainName string

func main() {
	var awsId, awsSecret, hostedZoneId, domainName string

	flag.StringVar(&awsId, "awsId", "", "AWS key ID")
	flag.StringVar(&awsSecret, "awsSecret", "", "AWS key secret")
	flag.StringVar(&hostedZoneId, "hostedZoneId", "", "Route 53 hosted zone ID")
	flag.StringVar(&domainName, "domainName", "", "domain name")

	flag.Parse()

	if awsId == "" || awsSecret == "" || hostedZoneId == "" || domainName == "" {
		log.Error().Msg("require -awsId -awsSecret -hostedZoneId -domainName")
	}

	configLog()

	sess, err := session.NewSession(&aws.Config{
		Credentials: credentials.NewStaticCredentials(awsId, awsSecret, ""),
	})

	if err != nil {
		panic(err)
	}

	firstRun := true

	for {
		err := func() (err error) {
			var currentIP, domainIP string
			defer func() {
				if re := recover(); re != nil {
					err = errors.New(fmt.Sprintf("panic currentIP=%v, domainIP=%v, err=%v\n", currentIP, domainIP, re))
				}
			}()

			err, currentIP = getCurrentIp()
			if err != nil {
				return err
			}

			err, domainIP = getDomainIp(sess, domainName, hostedZoneId)
			if err != nil {
				return err
			}

			if firstRun {
				firstRun = false
				log.Info().Str("currentIP", currentIP).Str("domainIP", domainIP).Msg("Started")
			}

			if currentIP != domainIP {
				log.Info().Str("currentIP", currentIP).Str("domainIP", domainIP).Msg("IP change detected")

				err = updateDomainIp(sess, domainName, currentIP, hostedZoneId)
				if err != nil {
					return err
				}

				log.Info().Str("currentIP", currentIP).Msg("Changed domainIP")
			}
			return nil
		}()

		if err != nil {
			log.Error().Err(err).Msg("loop error")
		}

		time.Sleep(time.Minute)
	}
}

func configLog() {
	luberjackWriter := &lumberjack.Logger{
		Filename:   "log.jsonl",
		MaxSize:    500, // megabytes
		MaxBackups: 100,
		MaxAge:     3650, //days
		Compress:   true, // disabled by default
	}

	multi := zerolog.MultiLevelWriter(zerolog.ConsoleWriter{Out: os.Stdout}, luberjackWriter)

	log.Logger = zerolog.New(multi).With().Timestamp().Logger()
}

func getCurrentIp() (error, string) {
	resp, err := http.Get("https://api.myip.com")
	if err != nil {
		return err, ""
	}

	defer resp.Body.Close()

	respBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err, ""
	}

	var respStruct struct {
		IP string `json:ip`
	}

	if err = json.Unmarshal(respBytes, &respStruct); err != nil {
		return err, ""
	}

	return nil, respStruct.IP
}

func getDomainIp(s *session.Session, name, hostedZoneId string) (error, string) {
	svc := route53.New(s)
	resp, err := svc.ListResourceRecordSets(&route53.ListResourceRecordSetsInput{
		HostedZoneId:    aws.String(hostedZoneId),
		StartRecordName: aws.String(name),
		StartRecordType: aws.String("A"),
		MaxItems:        aws.String("1"),
	})

	if err != nil {
		return err, ""
	}

	return nil, *resp.ResourceRecordSets[0].ResourceRecords[0].Value
}

func updateDomainIp(s *session.Session, name, ip, hostedZoneId string) error {
	svc := route53.New(s)
	params := &route53.ChangeResourceRecordSetsInput{
		ChangeBatch: &route53.ChangeBatch{
			Changes: []*route53.Change{
				{
					Action: aws.String("UPSERT"),
					ResourceRecordSet: &route53.ResourceRecordSet{
						Name: aws.String(name),
						Type: aws.String("A"),
						TTL:  aws.Int64(60),
						ResourceRecords: []*route53.ResourceRecord{
							{Value: aws.String(ip)},
						},
					},
				},
			},
		},
		HostedZoneId: aws.String(hostedZoneId),
	}
	_, err := svc.ChangeResourceRecordSets(params)
	return err
}
