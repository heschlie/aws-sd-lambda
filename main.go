package main

import (
	"context"
	"log"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/servicediscovery"
)

var TAG_PREFIX string

const REGISTER_EVENT = "EC2 Instance Launch Successful"
const UNREGISTER_EVENT = "EC2 Instance Terminate Successful"

func Handler(ctx context.Context, event events.AutoScalingEvent) {
	TAG_PREFIX = os.Getenv("TAG_PREFIX")
	ec2Id := event.Detail["EC2InstanceId"].(string)

	// Fire up the sessions we need.
	sess := session.Must(session.NewSession())
	sdSession := servicediscovery.New(sess)

	// Get the related ec2 instance.
	ec2Instance, err := findEc2Instance(sess, ec2Id)
	if err != nil {
		log.Fatalf("Could not find ec2 instance with ID: %v", ec2Id)
	}

	// Find our tags which follow "TAG_PREFIX/$namespace: $service". This will help us find our service ID later.
	serviceTags := findServiceTags(ec2Instance)
	if len(serviceTags) == 0 {
		log.Println("No service tags found, bailing")
		return
	}

	awsServices, err := findAwsServices(sdSession, serviceTags)
	if err != nil {
		log.Fatal(err)
	}

	// Iterate over the serviceTags and decide to either register or unregister based on event.
	for _, s := range awsServices {
		if event.DetailType == REGISTER_EVENT {
			log.Println("Found register event")
			registerService(sdSession, s, ec2Instance)
		} else if event.DetailType == UNREGISTER_EVENT {
			log.Println("Found deregister event")
			unregisterService(sdSession, s, ec2Instance)
		}
	}
}

// Find the ec2 Instance associated with this event.
func findEc2Instance(s *session.Session, id string) (*ec2.Instance, error) {
	ec2Session := ec2.New(s)
	ec2Filter := ec2.DescribeInstancesInput{
		InstanceIds: aws.StringSlice([]string{id}),
	}

	ec2Response, err := ec2Session.DescribeInstances(&ec2Filter)
	if err != nil {
		return nil, err
	}
	ec2Instance := ec2Response.Reservations[0].Instances[0]
	log.Printf("Found ec2 instance: %s\n", *ec2Instance.InstanceId)

	return ec2Instance, nil
}

// Find any service tags starting with the `TAG_PREFIX` constant.
func findServiceTags(ec2Instance *ec2.Instance) map[string][]string {
	services := make(map[string][]string)
	for _, tag := range ec2Instance.Tags {
		if strings.HasPrefix(*tag.Key, TAG_PREFIX) {
			log.Printf("Found tag '%s: %s' on ec2 %s\n", *tag.Key, *tag.Value, *ec2Instance.InstanceId)
			namespace := strings.Split(*tag.Key, "/")[1]
			tags := strings.Split(*tag.Value, ",")
			services[namespace] = tags
		}
	}

	return services
}

// Find the aws service discovery endpoints we want to register with.
func findAwsServices(sdSession *servicediscovery.ServiceDiscovery, serviceTags map[string][]string) ([]*servicediscovery.ServiceSummary, error) {
	// We first filter down by namespace, we want to get a mapping of namespace names to namespace IDs to
	// use later. If we get over 100 namespaces this will need to be revisited.
	nsResponse, err := sdSession.ListNamespaces(&servicediscovery.ListNamespacesInput{})
	if err != nil {
		return nil, err
	}
	nsIDmap := make(map[string]string)
	for _, ns := range nsResponse.Namespaces {
		nsIDmap[*ns.Name] = *ns.Id
	}


	var awsServices []*servicediscovery.ServiceSummary
	for ns, services := range serviceTags {
		// Filter down to the relevant namespace and build the request input.
		serviceFilter := servicediscovery.ServiceFilter{
			Condition: aws.String(servicediscovery.FilterConditionEq),
			Name: aws.String(servicediscovery.OperationFilterNameNamespaceId),
			Values: aws.StringSlice([]string{ nsIDmap[ns] }),

		}
		serviceInput := servicediscovery.ListServicesInput{
			Filters: []*servicediscovery.ServiceFilter{ &serviceFilter },
		}

		// List off the services that match our filter.
		servicesResponse, err := sdSession.ListServices(&serviceInput)
		if err != nil {
			return nil, err
		}

		// Match our tags to the services and save them for later
		for _, s := range servicesResponse.Services {
			for _, plosService := range services {
				if *s.Name == plosService {
					log.Printf("Found matching service: %s\n", *s.Name)
					awsServices = append(awsServices, s)
				}
			}
		}
	}

	return awsServices, nil
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
	log.Printf("registered %s with ip %s to service %s\n", *ec2Instance.InstanceId, *ec2Instance.PrivateIpAddress, *service.Name)
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
	log.Printf("deregistered %s with ip %s to service %s\n", *ec2Instance.InstanceId, *ec2Instance.PrivateIpAddress, *service.Name)
}

func main() {
	lambda.Start(Handler)
}
