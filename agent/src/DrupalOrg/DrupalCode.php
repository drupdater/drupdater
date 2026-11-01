<?php

declare(strict_types=1);

namespace Drupdater\Agent\DrupalOrg;

use Composer\Util\HttpDownloader;

/**
 * git.drupalcode.org, the GitLab holding every project's history and issue forks. It answers no
 * API call anonymously, so it needs DRUPALCODE_ACCESS_TOKEN.
 */
class DrupalCode
{
    public const TOKEN_VARIABLE = 'DRUPALCODE_ACCESS_TOKEN';

    public function __construct(
        private readonly HttpDownloader $http,
        private readonly string $token,
        private readonly string $baseUrl = 'https://git.drupalcode.org/api/v4',
    ) {
    }

    /**
     * Whether a commit on ref names the issue, which is how drupal.org records a fix.
     *
     * @throws \RuntimeException when the search fails
     */
    public function isFixedIn(string $project, string $issueId, string $ref): bool
    {
        $commits = $this->get(
            sprintf('projects/%s/-/search?%s', self::id('project/' . $project), http_build_query(['scope' => 'commits', 'search' => $issueId, 'ref' => $ref])),
            sprintf('failed to search %s for issue %s', $project, $issueId),
        );
        return is_array($commits) && $commits !== [];
    }

    /**
     * The merge requests into project from the issue's fork, newest first.
     *
     * @return list<ForkMergeRequest>
     *
     * @throws \RuntimeException when the fork or its merge requests cannot be read
     */
    public function issueForkMergeRequests(string $project, string $issueId): array
    {
        $fork = $this->get(sprintf('projects/%s', self::id('issue/' . $project . '-' . $issueId)), sprintf('failed to get the fork of issue %s', $issueId));
        if (!is_array($fork) || !is_int($fork['id'] ?? null)) {
            throw new \RuntimeException(sprintf('failed to get the fork of issue %s: no project id', $issueId));
        }
        // Filtered on the fork: every other merge request into the project is not the issue's.
        $mergeRequests = $this->get(
            sprintf('projects/%s/merge_requests?%s', self::id('project/' . $project), http_build_query(['source_project_id' => $fork['id']])),
            sprintf('failed to list the merge requests from the fork of issue %s', $issueId),
        );
        $found = [];
        foreach (is_array($mergeRequests) ? $mergeRequests : [] as $mergeRequest) {
            if (is_array($mergeRequest)) {
                $found[] = new ForkMergeRequest((string) ($mergeRequest['sha'] ?? ''), (string) ($mergeRequest['web_url'] ?? ''));
            }
        }
        return $found;
    }

    private function get(string $path, string $failure): mixed
    {
        try {
            $response = $this->http->get($this->baseUrl . '/' . $path, ['http' => ['header' => ['PRIVATE-TOKEN: ' . $this->token]]]);
            return json_decode((string) $response->getBody(), true, 512, JSON_THROW_ON_ERROR);
        }
        catch (\Exception $e) {
            throw new \RuntimeException($failure . ': ' . $e->getMessage(), 0, $e);
        }
    }

    // As GitLab's own client escapes a project path: the slash and the dot too.
    private static function id(string $path): string
    {
        return str_replace('.', '%2E', rawurlencode($path));
    }
}
