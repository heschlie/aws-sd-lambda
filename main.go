package main

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/servicediscovery"
)

const TAG_PREFIX = "plos/"

const REGISTER_EVENT = "EC2 Instance Launch Successful"
const UNREGISTER_EVENT = "EC2 Instance-terminate Lifecycle Action"

func Handler(ctx context.Context, event events.AutoScalingEvent) {
	asgName := event.Detail["AutoScalingGroupName"].(string)
	asgArn := event.Resources[0]
	ec2Id := event.Detail["EC2InstanceId"].(string)

	asgFilter := autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []*string{&asgName},
	}

	// Fire up the sessions we need.
	sess := session.Must(session.NewSession())
	asgSession := autoscaling.New(sess)
	sdSession := servicediscovery.New(sess)
	ec2Session := ec2.New(sess)

	asgResponse, err := asgSession.DescribeAutoScalingGroups(&asgFilter)
	if err != nil {
		log.Fatal(err)
	}

	// Find our tags which follow "plos/$namespace: $service". This will help us find our service ID later.
	services := make(map[string][]string)
	for _, asg := range asgResponse.AutoScalingGroups {
		if *asg.AutoScalingGroupARN == asgArn {
			for _, tag := range asg.Tags {
				if strings.HasPrefix(*tag.Key, TAG_PREFIX) {
					namespace := strings.Split(*tag.Key, "/")[1]
					services[namespace] = append(services[namespace], *tag.Value)
				}
			}
		}
	}

	// Iterate over our namespace and services, we filter down our services by namespace so we don't pull
	// all of our services. This will also allow us to register multiple services, under varying namespaces.
	var awsServices []*servicediscovery.ServiceSummary
	for ns, services := range services {
		// Filter down to the relevant namespace and build the request input.
		serviceFilter := servicediscovery.ServiceFilter{
			Condition: aws.String(servicediscovery.FilterConditionEq),
			Name: aws.String(servicediscovery.OperationFilterNameNamespaceId),
			Values: aws.StringSlice([]string{ ns }),

		}
		serviceInput := servicediscovery.ListServicesInput{
			Filters: []*servicediscovery.ServiceFilter{ &serviceFilter },
		}

		// List off the services that match our filter.
		servicesResponse, err := sdSession.ListServices(&serviceInput)
		if err != nil {
			log.Fatal(err)
		}

		// Match our tags to the services and save them for later
		for _, s := range servicesResponse.Services {
			for _, plosService := range services {
				if *s.Name == plosService {
					awsServices = append(awsServices, s)
				}
			}
		}
	}

	// We need to find our ec2 instance so we can grab the private IP.
	ec2Filter := ec2.DescribeInstancesInput{
		InstanceIds: []*string{&ec2Id},
	}

	ec2Response, err := ec2Session.DescribeInstances(&ec2Filter)
	if err != nil {
		log.Fatal(err)
	}
	ec2Instance := ec2Response.Reservations[0].Instances[0]

	// Iterate over the services and decide to either register or unregister based on event.
	for _, s := range awsServices {
		if event.DetailType == REGISTER_EVENT {
			registerService(sdSession, s, ec2Instance)
		} else if event.DetailType == UNREGISTER_EVENT {
			unregisterService(sdSession, s, ec2Instance)
		}
	}
}

func registerService(sd *servicediscovery.ServiceDiscovery, service *servicediscovery.ServiceSummary, ec2Instance *ec2.Instance) {
	input := servicediscovery.RegisterInstanceInput{
		ServiceId: service.Id,
		Attributes: map[string]*string{ "AWS_INSTANCE_IPV4": ec2Instance.PrivateIpAddress },
		CreatorRequestId: aws.String(time.Now().String()),
		InstanceId: ec2Instance.InstanceId,
	}

	_, err := sd.RegisterInstance(&input)
	if err != nil {
		log.Println(err)
	}
}

func unregisterService(sd *servicediscovery.ServiceDiscovery, service *servicediscovery.ServiceSummary, ec2Instance *ec2.Instance) {
	input := servicediscovery.DeregisterInstanceInput{
		InstanceId: ec2Instance.InstanceId,
		ServiceId: service.Id,
	}

	_, err := sd.DeregisterInstance(&input)
	if err != nil {
		log.Println(err)
	}
}

func main() {
	lambda.Start(Handler)
}
