<?php

declare(strict_types=1);

namespace Drupdater\App\Command;

use Drupdater\App\Failure;
use Drupdater\App\Git\Identity;
use Drupdater\App\Json;

/**
 * The request's author, whom the run's commits are by.
 */
final class Author
{
    public static function fromJson(mixed $author): Identity
    {
        $identity = new Identity(Json::string(Json::at($author, 'name')), Json::string(Json::at($author, 'email')));
        if ($identity->name === '' || $identity->email === '') {
            throw new Failure(Failure::INVALID_REQUEST, 'author needs a name and an email');
        }
        return $identity;
    }
}
