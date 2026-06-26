// Package internal contains application domain implementations that are not a
// supported external API.
//
// Placement rules:
//   - database/model owns persistence records and their storage serialization;
//   - internal/entities owns domain validation, mutations and JSON-field logic;
//   - internal/integrations owns provider transports without settings or DB access;
//   - top-level packages own infrastructure and process-level state;
//   - service orchestrates domains and persistence;
//   - api, sub and web adapt HTTP or subscription transports only.
package internal
