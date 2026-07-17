/*
Copyright (C) 2022-2024 ApeCloud Co., Ltd

This file is part of KubeBlocks project

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/

package utils

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/Masterminds/sprig/v3"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"sigs.k8s.io/yaml"

	dpv1alpha1 "github.com/apecloud/kubeblocks/apis/dataprotection/v1alpha1"
)

func TestS3StorageProviderAddressingStyle(t *testing.T) {
	provider := loadS3StorageProvider(t)
	forcePathStyle := provider.Spec.ParametersSchema.OpenAPIV3Schema.Properties["forcePathStyle"]
	require.Equal(t, "boolean", forcePathStyle.Type)
	require.Equal(t, []byte("false"), forcePathStyle.Default.Raw)

	testCases := []struct {
		name             string
		forcePathStyle   string
		expectSubdomain  bool
		expectedToolFlag string
	}{
		{
			name:             "defaults to virtual hosted style",
			expectSubdomain:  true,
			expectedToolFlag: "force_path_style = false",
		},
		{
			name:             "uses virtual hosted style explicitly",
			forcePathStyle:   "false",
			expectSubdomain:  true,
			expectedToolFlag: "force_path_style = false",
		},
		{
			name:             "uses path style explicitly",
			forcePathStyle:   "true",
			expectSubdomain:  false,
			expectedToolFlag: "force_path_style = true",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			parameters := map[string]string{
				"accessKeyId":           "access-key",
				"bucket":                "backup-bucket",
				"endpoint":              "https://s3.example.com",
				"forcePathStyle":        testCase.forcePathStyle,
				"geesefsMemoryLimit":    "512",
				"geesefsReadAheadLarge": "20480",
				"insecure":              "false",
				"mountOptions":          "",
				"region":                "us-east-1",
				"secretAccessKey":       "secret-key",
			}
			renderCtx := struct {
				Parameters         map[string]string
				CSIDriverSecretRef corev1.SecretReference
			}{
				Parameters: parameters,
				CSIDriverSecretRef: corev1.SecretReference{
					Name:      "s3-secret",
					Namespace: "kb-system",
				},
			}

			storageClassContent := renderStorageProviderTemplate(
				t, "storage-class", provider.Spec.StorageClassTemplate, renderCtx)
			storageClass := &storagev1.StorageClass{}
			require.NoError(t, yaml.Unmarshal([]byte(storageClassContent), storageClass))
			require.Equal(t, testCase.expectSubdomain,
				strings.Contains(storageClass.Parameters["options"], "--subdomain"))

			toolConfig := renderStorageProviderTemplate(
				t, "tool-config", provider.Spec.DatasafedConfigTemplate, renderCtx)
			require.Contains(t, toolConfig, testCase.expectedToolFlag)
		})
	}
}

func loadS3StorageProvider(t *testing.T) *dpv1alpha1.StorageProvider {
	t.Helper()

	chartPath := filepath.Join("..", "..", "..", "deploy", "helm")
	chart, err := loader.Load(chartPath)
	require.NoError(t, err)

	renderValues, err := chartutil.ToRenderValues(chart, chart.Values, chartutil.ReleaseOptions{
		Name:      "kubeblocks",
		Namespace: "kb-system",
		IsInstall: true,
	}, chartutil.DefaultCapabilities)
	require.NoError(t, err)

	rendered, err := engine.Render(chart, renderValues)
	require.NoError(t, err)

	var manifest string
	for name, content := range rendered {
		if strings.HasSuffix(name, "templates/storageprovider/s3.yaml") {
			manifest = content
			break
		}
	}
	require.NotEmpty(t, manifest)

	provider := &dpv1alpha1.StorageProvider{}
	require.NoError(t, yaml.Unmarshal([]byte(manifest), provider))
	require.Equal(t, "s3", provider.Name)
	return provider
}

func renderStorageProviderTemplate(t *testing.T, name, tpl string, renderCtx any) string {
	t.Helper()

	parsed, err := template.New(name).Funcs(sprig.TxtFuncMap()).Parse(tpl)
	require.NoError(t, err)

	var rendered bytes.Buffer
	require.NoError(t, parsed.Execute(&rendered, renderCtx))
	return rendered.String()
}
