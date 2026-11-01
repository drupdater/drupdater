<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests;

use Composer\Config;
use Composer\Downloader\TransportException;
use Composer\IO\NullIO;
use Composer\Util\Http\Response;
use Composer\Util\HttpDownloader;

/**
 * An HttpDownloader that answers from a table of URLs instead of the network, and records what
 * it was asked. A URL it does not know is a 404, as Composer reports one.
 */
final class FakeHttp extends HttpDownloader
{
    /** @var list<array{url: string, options: array<mixed>}> */
    public array $requests = [];

    /**
     * @param array<string, string|int> $routes the body each URL answers with, or the status code it fails with
     */
    public function __construct(private readonly array $routes = [])
    {
        parent::__construct(new NullIO(), new Config(false));
    }

    public function get(string $url, array $options = [])
    {
        $this->requests[] = ['url' => $url, 'options' => $options];
        return new Response(['url' => $url], 200, [], $this->body($url));
    }

    public function copy(string $url, string $to, array $options = [])
    {
        $this->requests[] = ['url' => $url, 'options' => $options];
        file_put_contents($to, $this->body($url));
        return new Response(['url' => $url], 200, [], '');
    }

    private function body(string $url): string
    {
        $route = $this->routes[$url] ?? 404;
        if (is_int($route)) {
            $e = new TransportException(sprintf('The "%s" file could not be downloaded (HTTP/2 %d )', $url, $route), $route);
            $e->setStatusCode($route);
            throw $e;
        }
        return $route;
    }
}
