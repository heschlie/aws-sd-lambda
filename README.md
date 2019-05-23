# AWS Service Discovery Lambda

This short script will watch your ASGs for Launch and Terminate events and
allow you to register and deregister those EC2 instances based on tags you have given
to them.

This only supports Route53 IP Address service discovery at the moment.

## How it works

The function lists off the tags of the EC2 instance that is being brought online or
terminated, looks for a tag or tags that are in the format:

```
TAG_PREFIX/NAMESPACE: SERVICE
or
TAG_PREFIX/NAMESPACE: SERVICE1, SERVICE2, ...
```

Then matches those tags to the Namespace, and Service to register against.

You pass an env var to the lambda function that is the prefix this function
will look for, as `TAG_PREFIX=PREFIX`, do not add the trailing `/`.

## IAM Roles

Only a few roles are needed for this script:

- AmazonEC2ReadOnlyAccess
- AWSCloudMapFullAccess
- CloudWatchLogsFullAccess

## CloudWatch event streams

You will need to watch a couple of Auto Scaling events, specifically:

- EC2 Instance Launch Successful
- EC2 Instance Terminate Successful

# Build

The included Makefile has a package target which will build and zip the
script for you to upload to AWS Lambda. The Function to target is `Handler`.

# Some gotchas

- Only supports Route53 IP, or A record discovery
- If you have more than 100 Namespaces this currently will have a chance to fail
- If you have more than 100 Services in a Namespace this could also fail
- The error logging isn't great
