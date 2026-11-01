<?php

declare(strict_types=1);

namespace Drupdater\Agent\DrupalOrg;

use Composer\Util\HttpDownloader;

/**
 * drupal.org's node API, for the issue a patch belongs to.
 */
class DrupalOrg
{
    public function __construct(private readonly HttpDownloader $http, private readonly string $baseUrl = 'https://www.drupal.org')
    {
    }

    /**
     * @throws \RuntimeException when the issue cannot be fetched or read
     */
    public function issue(string $id): Issue
    {
        $url = $this->baseUrl . '/api-d7/node/' . rawurlencode($id) . '.json';
        try {
            $response = $this->http->get($url);
        }
        catch (\Exception $e) {
            throw new \RuntimeException(sprintf('failed to fetch issue %s: %s', $id, $e->getMessage()), 0, $e);
        }
        // Status first: drupal.org answers an unknown node with an HTML page, not JSON.
        if ($response->getStatusCode() !== 200) {
            throw new \RuntimeException(sprintf('failed to fetch issue %s: unexpected status %d', $id, $response->getStatusCode()));
        }
        try {
            $node = json_decode((string) $response->getBody(), true, 512, JSON_THROW_ON_ERROR);
        }
        catch (\JsonException $e) {
            throw new \RuntimeException(sprintf('failed to decode issue %s: %s', $id, $e->getMessage()), 0, $e);
        }
        if (!is_array($node)) {
            throw new \RuntimeException(sprintf('failed to decode issue %s: not an object', $id));
        }
        return new Issue(
            self::string($node['nid'] ?? ''),
            self::string($node['title'] ?? ''),
            self::string($node['field_issue_status'] ?? ''),
            self::string($node['url'] ?? ''),
            self::string(is_array($node['field_project'] ?? null) ? ($node['field_project']['machine_name'] ?? '') : ''),
        );
    }

    private static function string(mixed $value): string
    {
        return is_scalar($value) ? (string) $value : '';
    }
}
