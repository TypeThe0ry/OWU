# Permit demo target

This deliberately small HTTP service is the only upstream resource registered by the local demo policy. It gives the integration suite a deterministic authorized destination without allowing arbitrary Internet targets.

Endpoints:

- `/` returns a small English HTML page and a host-only test cookie.
- `/api/whoami` returns request metadata that is safe to assert in tests.
- `/health` is the container health check.

The fixture is not exposed directly by the production design. Docker Compose places it on the internal demo network so requests must pass the Permit authorization and gateway checks.
