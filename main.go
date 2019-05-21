package main

import (
	"context"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"log"
)

const TAG = "plos/service"

func Handler(ctx context.Context, event events.AutoScalingEvent) {
	asgName := event.Detail["AutoScalingGroupName"].(string)
	asgArn := event.Resources[0]
	ec2Arn := event.Resources[1]

	asgFilter := autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []*string{ &asgName },
	}
	sess := session.Must(session.NewSession())
	asgSession := autoscaling.New(sess)

	response, err := asgSession.DescribeAutoScalingGroups(&asgFilter)
	if err != nil {
		log.Fatal(err)
	}

	var services []string
	for _, asg := range response.AutoScalingGroups {
		if *asg.AutoScalingGroupARN == asgArn {
			for _, tag := range asg.Tags {
				if *tag.Key == TAG {
					services = append(services, *tag.Value)
				}
			}
		}
	}


}

func main() {
	lambda.Start(Handler)
}
