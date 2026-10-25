# Native release acceptance contracts

CI runs every contract on darwin/arm64, darwin/amd64, linux/amd64 and windows/amd64, using both Go 1.26.10 and 1.27.3. Go 1.27.3 tests consume the binary extracted from the candidate archive. Temporary local Git repositories and generated assets replace personal inventories and live upstreams. Required tests must pass without skips; cross-compilation does not replace native execution.

The CLI contracts are in `acceptance/test_cli.py`, configuration repair contracts in `test_synthetic_config.py`, and Go unit contracts under `src/internal/`. Test names describe the protected business outcome; CI logs retain individual results.

| Required behavior | Representative contracts |
| --- | --- |
| Complete operational lifecycle and failure reporting | `test_operational_chain_publishes_edits_and_preserves_links_after_clean`, `test_operational_check_reports_missing_wrong_links_and_dirty_cache` |
| Version without configuration or Git | `test_version_requires_neither_configuration_nor_git` |
| Init preservation, home/project discovery and strict configuration | `test_init_creates_template_and_preserves_existing_configuration`, `test_default_init_and_config_discovery_use_orbit_home_directory`, `test_parent_config_discovery_uses_config_relative_paths`, config Go tests |
| All asset categories and selected-ref downloads | `test_vendor_sync_materializes_all_categories_without_commit_or_push`, `test_vendor_downloads_only_selected_ref_without_unrelated_history` |
| Canonical refs, legacy compatibility and strict controls | `TestDependencyRefCompatibilityAndStrictControls`, `test_ref_migration_reuses_installed_assets_and_cache` |
| Vendor opt-out preserves local assets, manifests, links and publication | `test_vendor_false_preserves_local_edits_manifest_and_publication`, `test_disabled_vendors_skip_unavailable_sources_and_missing_assets_fail_link` |
| Name/label comma and repeat selection, partial assembly/publication and invalid filters | `test_name_and_label_lists_update_only_selected_assets`, `test_invalid_selectors_fail_before_downloading` |
| Branch/tag/SHA identity | `test_branch_updates_but_commit_pinned_dependency_stays_unchanged`, `test_branch_tag_ambiguity_is_an_error`, `test_moved_tag_is_not_silently_updated`, annotated-tag and cached-tag contracts |
| Independent download failures and preflight protection | `test_partial_sync_failure_continues_other_dependencies_and_returns_failure`, `test_missing_selected_dependency_does_not_publish_partial_snapshot` |
| Offline publication and label isolation | `test_sync_publishes_local_assets_without_upstream_or_cache`, `test_label_sync_keeps_unselected_manifest_and_assets_unpublished` |
| Publication bytes, nested Git, staged/unmanaged data and Finder metadata | `test_mirror_publishes_file_bytes_without_nested_git_or_unrelated_content`, `test_git_attributes_cannot_silently_transform_mirrored_asset_bytes`, nested-Git, dirty-worktree and Finder-metadata contracts |
| Push retries, no empty commit, diverged history, force consent | `test_failed_push_is_retried_even_if_snapshot_has_not_changed`, `test_mirror_without_changes_creates_no_commit`, `test_diverged_history_is_not_automatically_merged_or_rebased`, `test_yes_alone_never_authorizes_history_rewrite` |
| Multiple links, idempotence, disabled/dangling cleanup and cache migration | `test_multiple_link_targets_share_the_same_asset`, `test_correct_link_is_idempotent`, `test_repo_links_replace_matching_old_cache_links_without_force`, disabled-link contracts |
| Real files, directories and symlinked parents remain protected | `test_force_link_never_deletes_real_directory_or_file`, `test_link_refuses_symlink_parent_without_writing_into_its_destination`, `test_disabling_link_preserves_foreign_links_and_real_entries_even_with_force` |
| Internal symlinks and byte/executable fidelity; escaping links and Git extensions rejected | `test_internal_symlink_is_preserved_as_a_symlink`, `test_relative_escaping_symlink_is_not_copied_into_mirror`, directory-alias and byte-publication contracts, `test_lfs_pointer_is_reported_as_unsupported`, `test_submodule_is_reported_as_unsupported` |
| Label cleanup, unsupported flags and selected ownership rejection | `test_clean_labels_and_unsupported_all_preserve_unselected_cache`, `test_clean_rejects_selected_cache_with_invalid_ownership` |
| Clean consent, ownership and assembled-link survival | `test_clean_declined_confirmation_preserves_all_assets`, `test_clean_removes_only_owned_cache_and_preserves_assembled_links`, `test_links_use_repo_assets_and_survive_cache_cleanup` |
| Coverage omissions, conflicts, backups, idempotence and custom settings | All ten `test_synthetic_config.py` contracts |
| Locks, subprocess termination, failed initialization retries and rollback | `test_concurrent_sync_does_not_corrupt_shared_cache`, `test_git_timeout_terminates_child_processes_and_releases_cache_lock`, failed-clone/cache contracts, `TestRollbackRestoresOldSnapshotOverPartialCopy` |
| Dependency names remain metadata; manifest/path ownership | `test_sync_never_uses_dependency_names_as_validation_paths`, `test_sync_dependency_name_cannot_collide_with_validation_manifest`, `test_partial_publish_rejects_manifest_ownership_overlap` |
| Online stable updates and installation preservation | `src/internal/updater/updater_test.go`: synthetic release HTTP server, exact native archive names, numeric versions/no downgrade, missing/duplicate release assets, SHA256 mismatch, unsafe archive rejection, temporary replacement/rollback and replacement of a running executable |
| Windows symlink permission remedy | `TestSymlinkPermissionErrorPreservesCauseAndExplainsWindowsRemedy` verifies the injected permission error and native platform guidance |

POSIX executable-bit assertions run within the full byte-preservation contract; Windows still runs its byte and publication assertions. Windows success-path tests require native symlink privileges. The permission-error unit contract uses a synthetic error without changing machine ACLs.

A successful candidate run uploads four package archives, per-package metadata, `CANDIDATE.json` and `SHA256SUMS`. Its source SHA must match the PR head. This is review evidence; publishing requires separate authorization and verification of the merged source SHA.
