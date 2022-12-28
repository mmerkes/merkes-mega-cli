package cmd

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/spf13/cobra"
)

var Endpoint, Filter, Partition, Region string

// cleanClustersCmd represents the cleanClusters command
var cleanClustersCmd = &cobra.Command{
	Use:   "cleanClusters",
	Short: "Clean up clusters and related resources",
	Long: `This command can be used to clean up clusters and related resources like nodegroups.

Not recommended for production use as this is destruction. You can use a filter to clean
up clusters matching a specific naming convention.`,
	Run: func(cmd *cobra.Command, args []string) {
		log.Printf("Endpoint: %s, Region: %s, Filter: %s, Partition: %s", Endpoint, Region, Filter, Partition)

		var configLoadOptions config.LoadOptionsFunc
		if Endpoint != "" {
			configLoadOptions = config.WithEndpointResolverWithOptions(getCustomEndpointResolver())
		} else {
			configLoadOptions = config.WithRegion(Region)
		}

		cfg, err := config.LoadDefaultConfig(context.TODO(), configLoadOptions)
		if err != nil {
			log.Fatal(err)
		}

		// Create an Amazon EKS service client
		client := eks.NewFromConfig(cfg)

		listClustersOutput, err := client.ListClusters(context.TODO(), &eks.ListClustersInput{})
		if err != nil {
			log.Fatal(err)
		}

		var r, emptyRegexp *regexp.Regexp
		if Filter != "" {
			r, _ = regexp.Compile(Filter)
		}

		for _, cluster := range listClustersOutput.Clusters {
			if r != emptyRegexp && !r.MatchString(cluster) {
				log.Printf("Skipping %s - does not match filter", cluster)
				continue
			}

			describeClusterOutput, err := client.DescribeCluster(context.TODO(), &eks.DescribeClusterInput{
				Name: aws.String(cluster),
			})
			if err != nil {
				log.Fatal(err)
			}
			if describeClusterOutput.Cluster.Status == types.ClusterStatusDeleting {
				log.Printf("Skipping %s - already deleting", cluster)
				continue
			}

			log.Printf("Cleaning up cluster %s", cluster)
			cleanUpNodegroups(client, cluster)
			_, err = client.DeleteCluster(context.TODO(), &eks.DeleteClusterInput{
				Name: aws.String(cluster),
			})
			if err != nil {
				log.Fatal(err)
			}
		}
	},
}

func cleanUpNodegroups(client *eks.Client, cluster string) {
	//func (c *Client) ListNodegroups(ctx context.Context, params *ListNodegroupsInput, optFns ...func(*Options)) (*ListNodegroupsOutput, error)
	listNodegroupsOutput, err := client.ListNodegroups(context.TODO(), &eks.ListNodegroupsInput{
		ClusterName: aws.String(cluster),
	})
	if err != nil {
		log.Fatal(err)
	}

	for _, nodegroup := range listNodegroupsOutput.Nodegroups {
		log.Printf("Cleaning up nodegroup %s in cluster %s", nodegroup, cluster)
		_, err = client.DeleteNodegroup(context.TODO(), &eks.DeleteNodegroupInput{
			ClusterName:   aws.String(cluster),
			NodegroupName: aws.String(nodegroup),
		})
		if err != nil {
			log.Fatal(err)
		}
	}

	for len(listNodegroupsOutput.Nodegroups) > 0 {
		log.Printf("%d nodegroups remaining for cluster %s", len(listNodegroupsOutput.Nodegroups), cluster)
		listNodegroupsOutput, err = client.ListNodegroups(context.TODO(), &eks.ListNodegroupsInput{
			ClusterName: aws.String(cluster),
		})
		if err != nil {
			log.Fatal(err)
		}

		time.Sleep(30 * time.Second)
	}
}

func getCustomEndpointResolver() aws.EndpointResolverWithOptionsFunc {
	return aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		if service == eks.ServiceID {
			return aws.Endpoint{
				PartitionID:   Partition,
				URL:           Endpoint,
				SigningRegion: region,
			}, nil
		}
		return aws.Endpoint{}, fmt.Errorf("unknown endpoint requested")
	})
}

func init() {
	rootCmd.AddCommand(cleanClustersCmd)

	cleanClustersCmd.Flags().StringVarP(&Endpoint, "endpoint", "e", "", "AWS EKS endpoint")
	cleanClustersCmd.Flags().StringVarP(&Partition, "partition", "p", "aws", "AWS partition")
	cleanClustersCmd.Flags().StringVarP(&Filter, "filter", "f", "", "Filter to use for matching cluster names")
	cleanClustersCmd.Flags().StringVarP(&Region, "region", "r", "us-west-2", "AWS EKS region. --endpoint will override region flag, if set")
}
