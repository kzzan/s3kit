package s3kit

import "testing"

func TestConfigNormalizedDefaultsRegion(t *testing.T) {
	cfg := Config{}

	got := cfg.normalized()

	if got.Region != defaultRegion {
		t.Fatalf("expected default region %q, got %q", defaultRegion, got.Region)
	}
}

func TestConfigValidateRequiresStaticCredentialPair(t *testing.T) {
	t.Parallel()

	tests := []Config{
		{AccessKeyID: "key"},
		{SecretAccessKey: "secret"},
	}

	for _, cfg := range tests {
		if err := cfg.validate(); err == nil {
			t.Fatalf("expected validation error for %#v", cfg)
		}
	}
}

func TestConfigValidateRejectsSessionTokenWithoutStaticCredentials(t *testing.T) {
	t.Parallel()

	err := (Config{SessionToken: "token"}).validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestConfigValidateAcceptsDefaultCredentialChain(t *testing.T) {
	t.Parallel()

	if err := (Config{Region: "us-west-2"}).validate(); err != nil {
		t.Fatalf("expected config to validate, got %v", err)
	}
}

func TestConfigValidateAcceptsCustomEndpoint(t *testing.T) {
	t.Parallel()

	err := (Config{
		Endpoint:        "https://minio.example.internal:9000",
		AccessKeyID:     "key",
		SecretAccessKey: "secret",
	}).validate()
	if err != nil {
		t.Fatalf("expected config to validate, got %v", err)
	}
}
