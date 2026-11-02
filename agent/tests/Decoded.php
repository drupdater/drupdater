<?php

declare(strict_types=1);

namespace Drupdater\Agent\Tests;

use Drupdater\Agent\Json;
use PHPUnit\Framework\Assert;

/**
 * JSON a test decodes, asserted to be the shape it reads it as.
 */
final class Decoded
{
    /**
     * The envelope a command printed, which must be a JSON object.
     *
     * @return array<string, mixed>
     */
    public static function envelope(string $json): array
    {
        $envelope = json_decode($json, true, 512, JSON_THROW_ON_ERROR);
        Assert::assertIsArray($envelope, $json);
        return Json::map($envelope);
    }

    public static function object(mixed $value): \stdClass
    {
        Assert::assertInstanceOf(\stdClass::class, $value);
        return $value;
    }
}
