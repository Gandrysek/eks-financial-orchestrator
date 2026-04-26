package collector

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	cetypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-logr/logr"
)

// CostExplorerAPI defines the subset of the AWS Cost Explorer client used
// by AWSCostFetcher. This interface enables testing with fakes.
type CostExplorerAPI interface {
	GetCostAndUsage(ctx context.Context, params *costexplorer.GetCostAndUsageInput, optFns ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error)
}

// S3API defines the subset of the AWS S3 client used by AWSCostFetcher.
type S3API interface {
	ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
}

// AWSCostFetcher retrieves cost data from AWS Cost Explorer and CUR (via S3).
type AWSCostFetcher struct {
	ceClient CostExplorerAPI
	s3Client S3API
	logger   logr.Logger
}

// NewAWSCostFetcher creates a new AWSCostFetcher with the given AWS clients
// and logger.
func NewAWSCostFetcher(ceClient CostExplorerAPI, s3Client S3API, logger logr.Logger) *AWSCostFetcher {
	return &AWSCostFetcher{
		ceClient: ceClient,
		s3Client: s3Client,
		logger:   logger.WithName("aws-cost-fetcher"),
	}
}

const (
	maxRetries    = 5
	dateFormat    = "2006-01-02"
	tagKeyPrefix  = "tag$"
)

// FetchCosts calls AWS Cost Explorer GetCostAndUsage grouped by service and
// tags, with exponential backoff for throttling errors.
func (f *AWSCostFetcher) FetchCosts(ctx context.Context, start, end time.Time) (*AWSCostData, error) {
	f.logger.V(1).Info("fetching AWS costs", "start", start, "end", end)

	var output *costexplorer.GetCostAndUsageOutput

	err := retryWithBackoff(ctx, maxRetries, func() error {
		input := &costexplorer.GetCostAndUsageInput{
			TimePeriod: &cetypes.DateInterval{
				Start: aws.String(start.Format(dateFormat)),
				End:   aws.String(end.Format(dateFormat)),
			},
			Granularity: cetypes.GranularityDaily,
			Metrics:     []string{"UnblendedCost"},
			GroupBy: []cetypes.GroupDefinition{
				{
					Type: cetypes.GroupDefinitionTypeDimension,
					Key:  aws.String("SERVICE"),
				},
				{
					Type: cetypes.GroupDefinitionTypeTag,
					Key:  aws.String("kubernetes.io/namespace"),
				},
			},
		}

		var callErr error
		output, callErr = f.ceClient.GetCostAndUsage(ctx, input)
		return callErr
	})

	if err != nil {
		return nil, fmt.Errorf("fetching cost explorer data: %w", err)
	}

	return f.parseCostExplorerOutput(output, start, end), nil
}

// parseCostExplorerOutput converts the Cost Explorer response into an
// AWSCostData struct, aggregating costs by service and by tag.
func (f *AWSCostFetcher) parseCostExplorerOutput(output *costexplorer.GetCostAndUsageOutput, start, end time.Time) *AWSCostData {
	data := &AWSCostData{
		StartTime: start,
		EndTime:   end,
		ByService: make(map[string]float64),
		ByTag:     make(map[string]float64),
	}

	for _, result := range output.ResultsByTime {
		for _, group := range result.Groups {
			amount := 0.0
			if cost, ok := group.Metrics["UnblendedCost"]; ok && cost.Amount != nil {
				parsed, err := strconv.ParseFloat(*cost.Amount, 64)
				if err == nil {
					amount = parsed
				}
			}

			data.TotalCost += amount

			// Parse group keys. The first key is the service dimension,
			// the second is the tag value.
			if len(group.Keys) >= 1 {
				service := group.Keys[0]
				data.ByService[service] += amount
			}
			if len(group.Keys) >= 2 {
				tagValue := group.Keys[1]
				// Strip the "tag$" prefix if present.
				tagValue = strings.TrimPrefix(tagValue, tagKeyPrefix)
				if tagValue != "" {
					data.ByTag[tagValue] += amount
				}
			}
		}
	}

	return data
}

// FetchCUR reads Cost and Usage Report data from S3 for the given time range.
// It lists objects under the specified bucket/prefix, downloads CSV files, and
// parses them into CURData.
func (f *AWSCostFetcher) FetchCUR(ctx context.Context, start, end time.Time, bucket, prefix string) (*CURData, error) {
	f.logger.V(1).Info("fetching CUR data", "bucket", bucket, "prefix", prefix, "start", start, "end", end)

	curData := &CURData{
		StartTime: start,
		EndTime:   end,
	}

	// List objects in the CUR S3 prefix.
	var objects []string
	err := retryWithBackoff(ctx, maxRetries, func() error {
		listInput := &s3.ListObjectsV2Input{
			Bucket: aws.String(bucket),
			Prefix: aws.String(prefix),
		}
		listOutput, listErr := f.s3Client.ListObjectsV2(ctx, listInput)
		if listErr != nil {
			return listErr
		}

		objects = nil
		for _, obj := range listOutput.Contents {
			if obj.Key != nil && strings.HasSuffix(*obj.Key, ".csv") {
				objects = append(objects, *obj.Key)
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("listing CUR objects in s3://%s/%s: %w", bucket, prefix, err)
	}

	// Download and parse each CSV file.
	for _, key := range objects {
		lineItems, err := f.downloadAndParseCUR(ctx, bucket, key)
		if err != nil {
			f.logger.Error(err, "failed to parse CUR file", "key", key)
			continue
		}
		for i := range lineItems {
			curData.TotalCost += lineItems[i].Cost
		}
		curData.LineItems = append(curData.LineItems, lineItems...)
	}

	f.logger.Info("CUR data fetched", "lineItems", len(curData.LineItems), "totalCost", curData.TotalCost)
	return curData, nil
}

// downloadAndParseCUR downloads a single CUR CSV file from S3 and parses it
// into CURLineItem records.
func (f *AWSCostFetcher) downloadAndParseCUR(ctx context.Context, bucket, key string) ([]CURLineItem, error) {
	var getOutput *s3.GetObjectOutput

	err := retryWithBackoff(ctx, maxRetries, func() error {
		input := &s3.GetObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		}
		var getErr error
		getOutput, getErr = f.s3Client.GetObject(ctx, input)
		return getErr
	})
	if err != nil {
		return nil, fmt.Errorf("downloading s3://%s/%s: %w", bucket, key, err)
	}
	defer getOutput.Body.Close()

	return parseCURCSV(getOutput.Body)
}

// parseCURCSV reads a CUR CSV from the given reader and returns line items.
// Expected columns (by header): lineItem/ResourceId, product/servicecode,
// lineItem/UsageType, pricing/purchaseOption, lineItem/UnblendedCost,
// and any resourceTags/* columns.
func parseCURCSV(r io.Reader) ([]CURLineItem, error) {
	reader := csv.NewReader(r)
	reader.LazyQuotes = true

	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("reading CSV header: %w", err)
	}

	// Build a column index for fast lookup.
	colIndex := make(map[string]int, len(header))
	for i, col := range header {
		colIndex[strings.TrimSpace(col)] = i
	}

	var items []CURLineItem
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return items, fmt.Errorf("reading CSV record: %w", err)
		}

		item := CURLineItem{
			ResourceID:     getCSVField(record, colIndex, "lineItem/ResourceId"),
			ServiceCode:    getCSVField(record, colIndex, "product/servicecode"),
			UsageType:      getCSVField(record, colIndex, "lineItem/UsageType"),
			PurchaseOption: getCSVField(record, colIndex, "pricing/purchaseOption"),
		}

		if costStr := getCSVField(record, colIndex, "lineItem/UnblendedCost"); costStr != "" {
			if parsed, parseErr := strconv.ParseFloat(costStr, 64); parseErr == nil {
				item.Cost = parsed
			}
		}

		// Collect resource tags.
		tags := make(map[string]string)
		for col, idx := range colIndex {
			if strings.HasPrefix(col, "resourceTags/") && idx < len(record) {
				tagKey := strings.TrimPrefix(col, "resourceTags/")
				if record[idx] != "" {
					tags[tagKey] = record[idx]
				}
			}
		}
		if len(tags) > 0 {
			item.Tags = tags
		}

		items = append(items, item)
	}

	return items, nil
}

// getCSVField safely retrieves a field from a CSV record by column name.
func getCSVField(record []string, colIndex map[string]int, column string) string {
	idx, ok := colIndex[column]
	if !ok || idx >= len(record) {
		return ""
	}
	return strings.TrimSpace(record[idx])
}
