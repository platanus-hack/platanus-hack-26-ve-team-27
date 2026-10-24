package com.team27.mama.setup

import android.content.ContentProviderOperation
import android.content.ContentResolver
import android.content.ContentUris
import android.provider.ContactsContract
import android.provider.ContactsContract.CommonDataKinds.Phone
import android.provider.ContactsContract.CommonDataKinds.StructuredName
import android.provider.ContactsContract.Data
import android.provider.ContactsContract.RawContacts

const val MAMA_CONTACT_NAME = "Mamá"

/** A raw contact as setup sees it: its ID, display name and phone numbers. */
data class RawContactInfo(val id: Long, val name: String?, val phones: List<String>)

/** What to do so exactly one "Mamá" with the Twilio number exists. */
sealed interface ContactPlan {
    /** Ours, already right. */
    data class Keep(val rawContactId: Long) : ContactPlan

    /** Ours, but the person edited it or the Twilio number changed: rewrite name and phone. */
    data class Rewrite(val rawContactId: Long) : ContactPlan

    /** Not saved by us yet, but a "Mamá" with this exact number exists (a reinstall): reuse it. */
    data class Adopt(val rawContactId: Long) : ContactPlan

    data object Insert : ContactPlan
}

/**
 * Decides how to upsert "Mamá" without duplicates. [ours] is the raw contact
 * this app saved before (null if it never did, or it was deleted). [namedMama]
 * are the other raw contacts called "Mamá". A "Mamá" with another number is
 * the person's real mother: it is never touched, and a separate contact is
 * inserted.
 */
fun planMamaContact(number: String, ours: RawContactInfo?, namedMama: List<RawContactInfo>): ContactPlan {
    fun RawContactInfo.hasNumber() = phones.any { PhoneNumbers.sameNumber(it, number) }
    if (ours != null) {
        val right = ours.name == MAMA_CONTACT_NAME && ours.phones.size == 1 && ours.hasNumber()
        return if (right) ContactPlan.Keep(ours.id) else ContactPlan.Rewrite(ours.id)
    }
    namedMama.firstOrNull { it.hasNumber() }?.let { return ContactPlan.Adopt(it.id) }
    return ContactPlan.Insert
}

/**
 * Saves the Twilio number as the contact "Mamá", in the phone's default account for new contacts, so the
 * call shows her name. Needs READ_CONTACTS and WRITE_CONTACTS. Returns the
 * raw contact ID to remember for the next run. Runs blocking; call it off the
 * main thread.
 */
class MamaContactWriter(private val resolver: ContentResolver) {

    fun upsert(number: String, savedRawContactId: Long?): Long {
        val ours = savedRawContactId?.let(::load)
        return when (val plan = planMamaContact(number, ours, namedMama())) {
            is ContactPlan.Keep -> plan.rawContactId
            is ContactPlan.Adopt -> plan.rawContactId
            is ContactPlan.Rewrite -> rewrite(plan.rawContactId, number)
            ContactPlan.Insert -> insert(number)
        }
    }

    /** The raw contact with [id], or null when it no longer exists or is marked deleted. */
    private fun load(id: Long): RawContactInfo? {
        val exists = resolver.query(
            ContentUris.withAppendedId(RawContacts.CONTENT_URI, id),
            arrayOf(RawContacts._ID),
            "${RawContacts.DELETED} = 0",
            null,
            null,
        )?.use { it.moveToFirst() } ?: false
        if (!exists) return null
        var name: String? = null
        val phones = mutableListOf<String>()
        resolver.query(
            Data.CONTENT_URI,
            arrayOf(Data.MIMETYPE, Data.DATA1),
            "${Data.RAW_CONTACT_ID} = ? AND ${Data.MIMETYPE} IN (?, ?)",
            arrayOf(id.toString(), StructuredName.CONTENT_ITEM_TYPE, Phone.CONTENT_ITEM_TYPE),
            null,
        )?.use { cursor ->
            while (cursor.moveToNext()) {
                val value = cursor.getString(1) ?: continue
                if (cursor.getString(0) == Phone.CONTENT_ITEM_TYPE) phones += value else name = value
            }
        }
        return RawContactInfo(id, name, phones)
    }

    /** Raw contacts whose phone rows belong to a contact displayed as "Mamá". */
    private fun namedMama(): List<RawContactInfo> {
        val phonesById = linkedMapOf<Long, MutableList<String>>()
        resolver.query(
            Data.CONTENT_URI,
            arrayOf(Data.RAW_CONTACT_ID, Phone.NUMBER),
            "${Data.MIMETYPE} = ? AND ${Data.DISPLAY_NAME} = ?",
            arrayOf(Phone.CONTENT_ITEM_TYPE, MAMA_CONTACT_NAME),
            null,
        )?.use { cursor ->
            while (cursor.moveToNext()) {
                val number = cursor.getString(1) ?: continue
                phonesById.getOrPut(cursor.getLong(0)) { mutableListOf() } += number
            }
        }
        return phonesById.map { (id, phones) -> RawContactInfo(id, MAMA_CONTACT_NAME, phones) }
    }

    private fun insert(number: String): Long {
        // No account columns: the provider files the contact under the phone's
        // default account for new contacts. An explicit null account means
        // "local", which Android 16 refuses when that default is a cloud
        // account ("Cannot add contacts to local or SIM accounts when default
        // account is set to cloud", seen on the demo Xiaomi).
        val ops = arrayListOf(
            // The provider rejects an insert with no values at all.
            ContentProviderOperation.newInsert(RawContacts.CONTENT_URI)
                .withValue(RawContacts.AGGREGATION_MODE, RawContacts.AGGREGATION_MODE_DEFAULT)
                .build(),
            nameRow().withValueBackReference(Data.RAW_CONTACT_ID, 0).build(),
            phoneRow(number).withValueBackReference(Data.RAW_CONTACT_ID, 0).build(),
        )
        val result = resolver.applyBatch(ContactsContract.AUTHORITY, ops)
        return ContentUris.parseId(checkNotNull(result[0].uri) { "contact insert returned no URI" })
    }

    private fun rewrite(id: Long, number: String): Long {
        val ops = arrayListOf(
            ContentProviderOperation.newDelete(Data.CONTENT_URI)
                .withSelection(
                    "${Data.RAW_CONTACT_ID} = ? AND ${Data.MIMETYPE} IN (?, ?)",
                    arrayOf(id.toString(), StructuredName.CONTENT_ITEM_TYPE, Phone.CONTENT_ITEM_TYPE),
                )
                .build(),
            nameRow().withValue(Data.RAW_CONTACT_ID, id).build(),
            phoneRow(number).withValue(Data.RAW_CONTACT_ID, id).build(),
        )
        resolver.applyBatch(ContactsContract.AUTHORITY, ops)
        return id
    }

    private fun nameRow() = ContentProviderOperation.newInsert(Data.CONTENT_URI)
        .withValue(Data.MIMETYPE, StructuredName.CONTENT_ITEM_TYPE)
        .withValue(StructuredName.DISPLAY_NAME, MAMA_CONTACT_NAME)
        .withValue(StructuredName.GIVEN_NAME, MAMA_CONTACT_NAME)

    private fun phoneRow(number: String) = ContentProviderOperation.newInsert(Data.CONTENT_URI)
        .withValue(Data.MIMETYPE, Phone.CONTENT_ITEM_TYPE)
        .withValue(Phone.NUMBER, number)
        .withValue(Phone.TYPE, Phone.TYPE_MOBILE)
}
