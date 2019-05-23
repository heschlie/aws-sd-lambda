package main

import (
	"context"
	"os"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
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
		log.WithError(err).WithField("ID", ec2Id).Fatal("Could not find ec2 instance")
	}

	// Find our tags which follow "TAG_PREFIX/$namespace: $service". This will help us find our service ID later.
	serviceTags := findServiceTags(ec2Instance)
	if len(serviceTags) == 0 {
		log.Info("No service tags found")
		return
	}

	awsServices, err := findAwsServices(sdSession, serviceTags)
	if err != nil {
		log.WithError(err).Fatal("Service lookup failed")
	}

	// Iterate over the serviceTags and decide to either register or unregister based on event.
	for _, s := range awsServices {
		if event.DetailType == REGISTER_EVENT {
			log.Info("Found register event")
			registerService(sdSession, s, ec2Instance)
		} else if event.DetailType == UNREGISTER_EVENT {
			log.Info("Found deregister event")
			deregisterService(sdSession, s, ec2Instance)
		}
	}
}

// Find the ec2 Instance associated with this event.
func findEc2Instance(s *session.Session, id string) (*ec2.Instance, error) {
	ec2Session := ec2.New(s)

	// Filter down to only the instance we need.
	ec2Filter := ec2.DescribeInstancesInput{
		InstanceIds: aws.StringSlice([]string{id}),
	}

	ec2Response, err := ec2Session.DescribeInstances(&ec2Filter)
	if err != nil {
		return nil, err
	}
	ec2Instance := ec2Response.Reservations[0].Instances[0]
	log.WithField("ID", *ec2Instance.InstanceId).Info("Found ec2 instance")

	return ec2Instance, nil
}

// Find any service tags starting with the `TAG_PREFIX` constant.
func findServiceTags(ec2Instance *ec2.Instance) map[string][]string {
	services := make(map[string][]string)
	for _, tag := range ec2Instance.Tags {
		if strings.HasPrefix(*tag.Key, TAG_PREFIX) {
			log.WithField("Instance", *ec2Instance.InstanceId).
				WithField(*tag.Key, *tag.Value).
				Info("Found tags")
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

	// Here we want to list off the services from each namespace we're interested in and capture their summaries.
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
					log.WithField("service", *s.Name).Info("Found matching service")
					awsServices = append(awsServices, s)
				}
			}
		}
	}

	return awsServices, nil
}

// registerService will register the given EC2 instance using the Instance ID as a unique ID and the IP address
// as the target for the service.
func registerService(sd *servicediscovery.ServiceDiscovery, service *servicediscovery.ServiceSummary, ec2Instance *ec2.Instance) {
	input := servicediscovery.RegisterInstanceInput{
		ServiceId: service.Id,
		Attributes: map[string]*string{ "AWS_INSTANCE_IPV4": ec2Instance.PrivateIpAddress },
		CreatorRequestId: aws.String(time.Now().String()),
		InstanceId: ec2Instance.InstanceId,
	}

	_, err := sd.RegisterInstance(&input)
	if err != nil {
		// If we error here just return, other services might register.
		log.WithError(err).
			WithField("ID", *ec2Instance.InstanceId).
			WithField("service", *service.Name).
			Error("Failed to register instance")
		return
	}
	log.WithField("InstanceID", *ec2Instance.InstanceId).
		WithField("IPv4", *ec2Instance.PrivateIpAddress).
		WithField("Service", *service.Name).
		Info("Successfully registered instance with service")
}

// deregisterService will deregister the given EC2 instance using the EC2 Instance ID from the given Service.
func deregisterService(sd *servicediscovery.ServiceDiscovery, service *servicediscovery.ServiceSummary, ec2Instance *ec2.Instance) {
	input := servicediscovery.DeregisterInstanceInput{
		InstanceId: ec2Instance.InstanceId,
		ServiceId: service.Id,
	}

	_, err := sd.DeregisterInstance(&input)
	if err != nil {
		// If we error here just return, other services might deregister.
		log.WithError(err).
			WithField("ID", *ec2Instance.InstanceId).
			WithField("service", *service.Name).
			Error("Failed to deregister instance")
		return
	}
	log.WithField("InstanceID", *ec2Instance.InstanceId).
		WithField("IPv4", *ec2Instance.PrivateIpAddress).
		WithField("Service", *service.Name).
		Info("Successfully deregistered instance with service")
}

func main() {
	lambda.Start(Handler)
}
