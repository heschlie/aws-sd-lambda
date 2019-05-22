package main

import (
	"context"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/servicediscovery"
	"github.com/aws/aws-sdk-go/service/ec2"
	"log"
	"strings"
	"time"
)

const TAG_PREFIX = "plos/"

func Handler(ctx context.Context, event events.AutoScalingEvent) {
	asgName := event.Detail["AutoScalingGroupName"].(string)
	asgArn := event.Resources[0]
	ec2Id := event.Detail["EC2InstanceId"].(string)

	asgFilter := autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []*string{ &asgName },
	}

	sess := session.Must(session.NewSession())
	asgSession := autoscaling.New(sess)
	sdSession := servicediscovery.New(sess)
	ec2Session := ec2.New(sess)

	asgResponse, err := asgSession.DescribeAutoScalingGroups(&asgFilter)
	if err != nil {
		log.Fatal(err)
	}

	var services map[string]string
	for _, asg := range asgResponse.AutoScalingGroups {
		if *asg.AutoScalingGroupARN == asgArn {
			for _, tag := range asg.Tags {
				if strings.HasPrefix(*tag.Key, TAG_PREFIX) {

				}
			}
		}
	}

	ec2Filter := ec2.DescribeInstancesInput{
		InstanceIds: []*string{ &ec2Id },
	}

	ec2Response, err := ec2Session.DescribeInstances(&ec2Filter)
	if err != nil {
		log.Fatal(err)
	}
	ec2Ip := ec2Response.Reservations[0].Instances[0].PrivateIpAddress

}

func registerService(sd *servicediscovery.ServiceDiscovery, service string, instance string, privateIp *string) {

	serviceFilter := servicediscovery.ServiceFilter{
		Condition: aws.String(servicediscovery.FilterConditionEq),
		Name: aws.String(servicediscovery.OperationFilterNameNamespaceId),
		Values: aws.StringSlice([]string{ })

	}
	serviceInput := servicediscovery.ListServicesInput{

	}

	sd.ListServices()

	input := servicediscovery.RegisterInstanceInput{
		Attributes: map[string]*string{ "AWS_INSTANCE_IPV4": privateIp },
		CreatorRequestId: aws.String(time.Now().String()),
	}

}

func unregisterService(sd *servicediscovery.ServiceDiscovery, service string, instance string) {

}

func main() {
	lambda.Start(Handler)
}
