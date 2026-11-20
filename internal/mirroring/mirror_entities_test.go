package mirroring

import (
	"context"
	"errors"
	"testing"

	"github.com/boxboxjason/gitlab-sync/internal/telemetry"
	"github.com/boxboxjason/gitlab-sync/pkg/helpers"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestMirrorProjectEntitiesRecordsEntities(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	t.Cleanup(func() {
		otel.SetMeterProvider(noop.NewMeterProvider())
		helpers.ResetReported()
	})

	project := &gitlab.Project{HTTPURLToRepo: "https://gitlab.example.com/group/project.git"}

	mirrorProjectEntities(
		telemetry.EntityIssue,
		project,
		project,
		func(*gitlab.Project) (map[string]struct{}, error) {
			return map[string]struct{}{"existing": {}}, nil
		},
		func(*gitlab.Project) ([]string, error) {
			return []string{"existing", "new", "broken"}, nil
		},
		func(key string) string { return key },
		func(_ *gitlab.Project, key string) error {
			if key == "broken" {
				return errors.New("boom")
			}

			return nil
		},
	)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	got := map[string]int64{}

	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "gitlab_sync.entities" {
				continue
			}

			for _, point := range m.Data.(metricdata.Sum[int64]).DataPoints {
				if entity, _ := point.Attributes.Value(telemetry.AttrEntity); entity != attribute.StringValue(telemetry.EntityIssue) {
					t.Errorf("entity = %q, want %q", entity.AsString(), telemetry.EntityIssue)
				}

				operation, _ := point.Attributes.Value(telemetry.AttrOperation)
				outcome, _ := point.Attributes.Value(telemetry.AttrOutcome)
				got[operation.AsString()+"/"+outcome.AsString()] += point.Value
			}
		}
	}

	want := map[string]int64{"skip/success": 1, "create/success": 1, "create/failure": 1}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("issues %s = %d, want %d (all: %v)", key, got[key], value, got)
		}
	}
}
